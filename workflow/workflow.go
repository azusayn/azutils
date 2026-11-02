package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
)

type WorkflowEvent struct {
	ID      string
	Payload json.RawMessage
}

type NodeInputConfig map[string]string

type NodeConfig struct {
	Inputs      NodeInputConfig
	NextNodeIDs []string
}

type Workflow struct {
	store             WorkflowStore
	once              *sync.Once
	eventCh           <-chan *WorkflowEvent
	callbackByEventID map[string]func(context.Context, any) error
	logger            *slog.Logger
	// nodeConfigs maps node ID to its node config.
	nodeConfigs   map[string]NodeConfig
	subscriberMap *subscriberMap
}

func NewWorkflow(configs map[string]NodeConfig, store WorkflowStore) *Workflow {
	return &Workflow{
		store:             store,
		callbackByEventID: make(map[string]func(context.Context, any) error),
		logger:            slog.Default(),
		nodeConfigs:       configs,
		subscriberMap:     newSubscriberMap(store),
	}
}

// StartEventPoller 启动后台协程对工作流上的事件通道进行监听，并分发事件给注册的事件处理器。
func (w *Workflow) StartEventPoller(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()

			case event := <-w.eventCh:
				if err := w.handleWorkflowEvent(ctx, event); err != nil {
					slog.ErrorContext(ctx, "failed to process workflow event", slog.Any("err", err))
				}
			}
		}
	})

	return g.Wait()
}

func (w *Workflow) handleWorkflowEvent(ctx context.Context, event *WorkflowEvent) error {
	fn := func(ctx context.Context, workflowRunID, nodeID string, interestEventIDs map[string]struct{}) error {
		_, ok := interestEventIDs[event.ID]
		if !ok {
			return nil
		}

		callback, err := w.getRegisteredCallback(event.ID)
		if err != nil {
			w.logger.ErrorContext(ctx, "failed to get registered callback", slog.Any("err", err))
			return nil
		}

		go func() {
			if err := func() error {
				storeWorkflowRun, err := w.store.GetWorkflowRun(ctx, workflowRunID)
				if err != nil {
					return err
				}

				p := NewVariablePool(w.nodeConfigs)
				if err := p.Load(storeWorkflowRun.Data); err != nil {
					return err
				}

				var interestList map[string]map[string]struct{}
				if err := json.Unmarshal([]byte(storeWorkflowRun.InterestList), &interestList); err != nil {
					return err
				}

				workflowRun := &WorkflowRun{
					ID:                workflowRunID,
					nodeConfigs:       w.nodeConfigs,
					store:             w.store,
					subscriberMap:     w.subscriberMap,
					Pool:              p,
					currentNodeStatus: NodeStatusPause,
					currentNodeID:     storeWorkflowRun.CurrentNodeID,
				}

				ctx = withEventID(ctx, event.ID)
				ctx = withWorkflowRun(ctx, workflowRun)
				ctx = withNodeID(ctx, nodeID)

				if err := callback(ctx, event.Payload); err != nil {
					return err
				}
				return nil
			}(); err != nil {
				w.logger.ErrorContext(
					ctx,
					"failed to process workflow event callback",
					slog.Any("err", err),
					slog.Any("workflow_run_id", workflowRunID),
					slog.Any("node_id", nodeID),
				)
			}
		}()

		return nil
	}

	return w.subscriberMap.foreach(ctx, fn)
}

func (w *Workflow) getRegisteredCallback(eventID string) (func(context.Context, any) error, error) {
	callback, ok := w.callbackByEventID[eventID]
	if !ok {
		return nil, fmt.Errorf("callback name not found for event: %q", eventID)
	}

	return callback, nil
}

func (w *Workflow) RegisterCallback(eventID string, callback func(context.Context, any) error) error {
	if _, ok := w.callbackByEventID[eventID]; ok {
		return fmt.Errorf("event %s already registered", eventID)
	}

	w.callbackByEventID[eventID] = callback

	return nil
}

type WorkflowRun struct {
	ID                string
	Pool              *VariablePool
	nodeConfigs       map[string]NodeConfig
	store             WorkflowStore
	subscriberMap     *subscriberMap
	currentNodeID     string
	currentNodeStatus NodeStatus
}

// Run 根据输入条件执行事件联动链路。
//
// TODO: 提供变量池初始化参数而不是让调用者直接操纵 pool.
func (w *Workflow) Run(ctx context.Context, initPoolFunc func(pool *VariablePool)) (*WorkflowRun, error) {
	storeWorkflowRun, err := w.store.CreateWorkflowRun(ctx)
	if err != nil {
		return nil, err
	}

	p := NewVariablePool(w.nodeConfigs)
	initPoolFunc(p)

	workflowRun := &WorkflowRun{
		ID:            storeWorkflowRun.ID,
		store:         w.store,
		nodeConfigs:   w.nodeConfigs,
		subscriberMap: w.subscriberMap,
		Pool:          p,
		currentNodeID: RootNodeID,
	}

	go func() {
		if err := workflowRun.executeOne(ctx); err != nil {
			w.logger.ErrorContext(ctx, "failed to run workflow", slog.Any("err", err))
		}
	}()

	return workflowRun, nil
}

func Skip(ctx context.Context) error {
	workflowRun, err := extractWorkflowRun(ctx)
	if err != nil {
		return err
	}
	workflowRun.currentNodeStatus = NodeStatusSkipped
	return nil
}

func WaitFor(ctx context.Context, eventID string) error {
	workflowRun, err := extractWorkflowRun(ctx)
	if err != nil {
		return err
	}
	nodeID, err := extractNodeID(ctx)
	if err != nil {
		return err
	}

	workflowRun.currentNodeStatus = NodeStatusPause

	return workflowRun.subscriberMap.set(ctx, workflowRun.ID, nodeID, eventID)
}

func Resume(ctx context.Context) error {
	workflowRun, err := extractWorkflowRun(ctx)
	if err != nil {
		return err
	}
	nodeID, err := extractNodeID(ctx)
	if err != nil {
		return err
	}
	eventID, err := extractEventID(ctx)
	if err != nil {
		return err
	}

	if workflowRun.currentNodeStatus != NodeStatusPause {
		return errors.New("cannot resume unsuspend node")
	}
	workflowRun.subscriberMap.unset(ctx, workflowRun.ID, nodeID, eventID)
	workflowRun.currentNodeStatus = NodeStatusCompleted

	if err := workflowRun.store.ResumeWrokflowRun(ctx, workflowRun.ID); err != nil {
		return fmt.Errorf("failed to resume: %w", err)
	}

	return workflowRun.executeOne(ctx)
}

func (r *WorkflowRun) executeOne(ctx context.Context) error {
	act, ok := Lookup(r.currentNodeID)
	if !ok {
		return fmt.Errorf("linkage action not registered: actionType=%s", r.currentNodeID)
	}

	ctx = withNodeID(ctx, r.currentNodeID)
	ctx = withWorkflowPool(ctx, r.Pool)
	ctx = withWorkflowRun(ctx, r)

	var execErr error
	if err := act.Execute(ctx); err != nil {
		r.currentNodeStatus = NodeStatusFailed
		execErr = err
	} else if r.currentNodeStatus != NodeStatusPause && r.currentNodeStatus != NodeStatusSkipped {
		r.currentNodeStatus = NodeStatusCompleted
	}

	bytes, err := r.Pool.Dump()
	if err != nil {
		return err
	}

	if err := r.store.UpdateWorkflowRunStatus(ctx, r.ID, r.currentNodeID, r.currentNodeStatus, execErr, bytes); err != nil {
		return err
	}

	if r.currentNodeStatus != NodeStatusCompleted {
		return nil
	}

	nextNodeIDs := r.nodeConfigs[r.currentNodeID].NextNodeIDs
	for _, nextNodeID := range nextNodeIDs {
		r.currentNodeID = nextNodeID
		if err := r.executeOne(ctx); err != nil {
			return err
		}
	}

	return nil
}

type subscriberMap struct {
	// data maps subscriberID (workflowRunID + nodeID) to eventIDs.
	data  map[string]map[string]struct{}
	store WorkflowStore
	mu    sync.Mutex
}

func newSubscriberMap(store WorkflowStore) *subscriberMap {
	return &subscriberMap{
		data:  make(map[string]map[string]struct{}),
		store: store,
	}
}

func (m *subscriberMap) set(ctx context.Context, workflowRunID string, nodeID string, eventID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	subscriberID := buildSubscriberID(workflowRunID, nodeID)

	if _, ok := m.data[subscriberID]; ok {
		return fmt.Errorf("subscriber %s already subscribed", subscriberID)
	}

	if _, ok := m.data[subscriberID]; !ok {
		m.data[subscriberID] = make(map[string]struct{})
	}
	m.data[subscriberID][eventID] = struct{}{}

	// TODO: outbox 保证一致性
	return m.store.UpdateInterestList(ctx, workflowRunID, m.data)
}

func (m *subscriberMap) unset(ctx context.Context, workflowRunID string, nodeID string, eventID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	subscriberID := buildSubscriberID(workflowRunID, nodeID)
	if _, ok := m.data[subscriberID]; !ok {
		return fmt.Errorf("subscriber %s not found", subscriberID)
	}
	delete(m.data[subscriberID], eventID)

	return m.store.UpdateInterestList(ctx, workflowRunID, m.data)
}

func (m *subscriberMap) foreach(
	ctx context.Context,
	fn func(ctx context.Context, workflowRunID, nodeID string, interestEventIDs map[string]struct{}) error,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for subscriberID, eventIDs := range m.data {
		workflowRunID, nodeID, err := parseSubscriberID(subscriberID)
		if err != nil {
			return err
		}
		if err := fn(ctx, workflowRunID, nodeID, eventIDs); err != nil {
			return err
		}
	}

	return nil
}

// buildSubscriberID 构建感兴趣事件在每个工作流下的唯一标识。
func buildSubscriberID(workflowRunID string, nodeID string) string {
	return fmt.Sprintf("%s:%s", workflowRunID, nodeID)
}

func parseSubscriberID(subscriberID string) (workflowRunID, nodeID string, err error) {
	tokens := strings.Split(subscriberID, ":")
	if len(tokens) != 2 {
		return "", "", fmt.Errorf("invalid subscriber ID %q", subscriberID)
	}
	return tokens[0], tokens[1], nil
}
