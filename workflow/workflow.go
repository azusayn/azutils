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

const (
	readyChannelCapacity         = 2000
	workflowEventChannelCapacity = 1000
)

type WorkflowEvent struct {
	ID            string
	WorkflowRunID string
	NodeID        string
	Payload       json.RawMessage
}

type NodeInputConfig map[string]string

type NodeConfig struct {
	Inputs      NodeInputConfig
	NextNodeIDs []string
}

type Workflow struct {
	once   sync.Once
	logger *slog.Logger
	// nodeConfigs maps node ID to its node config.
	nodeConfigs     map[string]NodeConfig
	nodeExecutorNum int

	store             WorkflowStore
	readyCh           chan *ReadyNodeContext
	eventCh           <-chan *WorkflowEvent
	subscriberMap     *subscriberMap
	callbackByEventID map[string]func(context.Context, any) error
}

type WorkflowOption func(*Workflow) error

func WithLogger(logger *slog.Logger) WorkflowOption {
	return func(w *Workflow) error {
		w.logger = logger
		return nil
	}
}

func WithNodeExecutorNum(executorNum int) WorkflowOption {
	return func(w *Workflow) error {
		w.nodeExecutorNum = executorNum
		return nil
	}
}

func NewWorkflow(configs map[string]NodeConfig, store WorkflowStore, opts ...WorkflowOption) *Workflow {
	w := &Workflow{
		store:             store,
		callbackByEventID: make(map[string]func(context.Context, any) error),
		logger:            slog.Default(),
		nodeConfigs:       configs,
		nodeExecutorNum:   1,
		subscriberMap:     newSubscriberMap(store),
		readyCh:           make(chan *ReadyNodeContext, readyChannelCapacity),
		eventCh:           make(chan *WorkflowEvent, workflowEventChannelCapacity),
	}

	for _, opt := range opts {
		opt(w)
	}

	return w
}

type ReadyNodeContext struct {
	NodeID      string
	WorkflowRun *WorkflowRun
}

func (w *Workflow) RecoverWorkflowRuns(ctx context.Context) error {
	workflowRunModels, err := w.store.ListPendingWorkflowRuns(ctx, len(w.nodeConfigs))
	if err != nil {
		return err
	}

	for _, workflowRunModel := range workflowRunModels {
		workflowRun, err := w.convertToWorkflowRun(workflowRunModel)
		if err != nil {
			w.logger.ErrorContext(
				ctx,
				"failed to recover workflow run",
				slog.Any("err", err),
			)
			continue
		}
		readyNodeIDs, err := getReadyNodeIDs(w.nodeConfigs, workflowRunModel.NodeExecutions)
		if err != nil {
			return err
		}
		for _, nodeID := range readyNodeIDs {
			w.readyCh <- &ReadyNodeContext{
				NodeID:      nodeID,
				WorkflowRun: workflowRun,
			}
		}
	}

	return nil
}

func (w *Workflow) StartNodeExecutors(ctx context.Context) error {
	startExecutor := func(ctx context.Context) error {
		for {
			select {
			case nodeContext := <-w.readyCh:
				nodeStatus, err := nodeContext.WorkflowRun.execute(ctx, nodeContext.NodeID)
				if err != nil {
					w.logger.ErrorContext(
						ctx,
						"failed to execute node",
						slog.Any("node_id", nodeContext.NodeID),
						slog.Any("err", err),
					)
					continue
				}

				// TODO: scheduler.
				if nodeStatus != NodeStatusCompleted {
					continue
				}
				nodeConfig, ok := w.nodeConfigs[nodeContext.NodeID]
				if !ok {
					return fmt.Errorf("failed to get node config for %q", nodeContext.NodeID)
				}
				for _, nextNodeID := range nodeConfig.NextNodeIDs {
					w.readyCh <- &ReadyNodeContext{
						WorkflowRun: nodeContext.WorkflowRun,
						NodeID:      nextNodeID,
					}
				}

			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, w.nodeExecutorNum)

	for range w.nodeExecutorNum {
		wg.Go(func() {
			errCh <- startExecutor(ctx)
		})
	}

	wg.Wait()

	var merr error
	for range w.nodeExecutorNum {
		merr = errors.Join(merr, <-errCh)
	}

	return merr
}

func (r *WorkflowRun) execute(ctx context.Context, nodeID string) (NodeStatus, error) {
	// TODO: distinguish between node types and node IDs.
	act, ok := Lookup(nodeID)
	if !ok {
		return NodeStatusPending, fmt.Errorf("node type %q not registered", nodeID)
	}

	ctx = withNodeID(ctx, nodeID)
	ctx = withWorkflowPool(ctx, r.Pool)
	ctx = withWorkflowRun(ctx, r)

	nodeStatus := NodeStatusPending
	ctx = withNodeStatus(ctx, &nodeStatus)

	var execErr error
	if err := act.Execute(ctx); err != nil {
		nodeStatus = NodeStatusFailed
		execErr = err
	} else if nodeStatus != NodeStatusPause && nodeStatus != NodeStatusSkipped {
		nodeStatus = NodeStatusCompleted
	}

	bytes, err := r.Pool.Dump()
	if err != nil {
		return nodeStatus, err
	}

	nodeExecution := map[string]NodeStatus{
		nodeID: nodeStatus,
	}

	if err := r.store.UpdateWorkflowRunStatus(ctx, r.ID, nodeExecution, execErr, bytes); err != nil {
		return nodeStatus, err
	}

	return nodeStatus, nil
}

func getReadyNodeIDs(configs map[string]NodeConfig, executions map[string]NodeStatus) ([]string, error) {
	readyNodeIDs := make([]string, 0)

	var dfs func(string) error
	dfs = func(nodeID string) error {
		status, ok := executions[nodeID]
		if !ok {
			return fmt.Errorf("failed to get status for %q", nodeID)
		}

		if status == NodeStatusPause || status == NodeStatusFailed || status == NodeStatusSkipped {
			return nil
		}

		switch status {
		case NodeStatusPause,
			NodeStatusFailed,
			NodeStatusSkipped:
			return nil

		case NodeStatusPending:
			readyNodeIDs = append(readyNodeIDs, nodeID)

		default:
		}

		config, ok := configs[nodeID]
		if !ok {
			return fmt.Errorf("failed to get config for %q", nodeID)
		}

		for _, nextNodeID := range config.NextNodeIDs {
			if err := dfs(nextNodeID); err != nil {
				return err
			}
		}

		return nil
	}

	if err := dfs(RootNodeID); err != nil {
		return nil, err
	}

	return readyNodeIDs, nil
}

func (w *Workflow) convertToWorkflowRun(workflowRunModel *WorkflowRunModel) (*WorkflowRun, error) {
	if workflowRunModel == nil {
		return nil, errors.New("nil workflow run model")
	}

	pool := NewVariablePool(w.nodeConfigs)
	if err := pool.Load(workflowRunModel.Data); err != nil {
		return nil, err
	}

	return &WorkflowRun{
		ID:            workflowRunModel.ID,
		nodeConfigs:   w.nodeConfigs,
		store:         w.store,
		subscriberMap: w.subscriberMap,
		readyCh:       w.readyCh,
		Pool:          pool,
	}, nil
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
	fn := func(ctx context.Context) error {
		callback, err := w.getRegisteredCallback(event.ID)
		if err != nil {
			w.logger.ErrorContext(ctx, "failed to get registered callback", slog.Any("err", err))
			return nil
		}

		workflowRunModel, err := w.store.GetWorkflowRun(ctx, event.WorkflowRunID)
		if err != nil {
			return err
		}

		status, ok := workflowRunModel.NodeExecutions[event.NodeID]
		if !ok {
			return errors.New("failed to get node status")
		}

		workflowRun, err := w.convertToWorkflowRun(workflowRunModel)
		if err != nil {
			return err
		}

		ctx = withEventID(ctx, event.ID)
		ctx = withWorkflowRun(ctx, workflowRun)
		ctx = withNodeID(ctx, event.NodeID)
		ctx = withNodeStatus(ctx, &status)

		if err := callback(ctx, event.Payload); err != nil {
			return err
		}
		return nil
	}

	return w.subscriberMap.notify(ctx, event.ID, event.WorkflowRunID, event.NodeID, fn)
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
	ID            string
	nodeConfigs   map[string]NodeConfig
	Pool          *VariablePool
	store         WorkflowStore
	subscriberMap *subscriberMap
	readyCh       chan *ReadyNodeContext
}

// Run 根据输入条件执行事件联动链路。
//
// TODO: 提供变量池初始化参数而不是让调用者直接操纵 pool.
func (w *Workflow) Run(ctx context.Context, initPoolFunc func(pool *VariablePool)) (*WorkflowRun, error) {
	p := NewVariablePool(w.nodeConfigs)
	initPoolFunc(p)

	data, err := p.Dump()
	if err != nil {
		return nil, err
	}

	nodeExecutions := make(map[string]NodeStatus)
	for nodeID := range w.nodeConfigs {
		nodeExecutions[nodeID] = NodeStatusPending
	}

	workflowRunModel, err := w.store.CreateWorkflowRun(ctx, &WorkflowRunModel{
		Data:           data,
		CompletedNodes: 0,
		NodeExecutions: nodeExecutions,
	})
	if err != nil {
		return nil, err
	}

	workflowRun, err := w.convertToWorkflowRun(workflowRunModel)
	if err != nil {
		return nil, err
	}

	w.readyCh <- &ReadyNodeContext{
		NodeID:      RootNodeID,
		WorkflowRun: workflowRun,
	}

	return workflowRun, nil
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

func (m *subscriberMap) notify(
	ctx context.Context,
	targetWorkflowID,
	targetNodeID,
	targetEventID string,
	fn func(ctx context.Context) error,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for subscriberID, eventIDs := range m.data {
		workflowRunID, nodeID, err := parseSubscriberID(subscriberID)
		if err != nil {
			return err
		}
		if (workflowRunID != targetWorkflowID) || (nodeID != targetNodeID) {
			continue
		}

		if _, ok := eventIDs[targetEventID]; !ok {
			continue
		}

		if err := fn(ctx); err != nil {
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
