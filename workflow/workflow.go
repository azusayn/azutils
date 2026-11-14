package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	readyChannelCapacity         = 2000
	workflowEventChannelCapacity = 1000
	defaultNodeExecutorNum       = 4
	// TODO: support per-node timeout policy.
	defaultNodeExecutionTimeout = 3 * time.Second
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
	InDegree    int
	NextNodeIDs []string
}

type Workflow struct {
	ID   string
	Name string

	logger *slog.Logger
	// nodeConfigs maps node ID to its node config.
	nodeConfigs          map[string]*NodeConfig
	nodeExecutorNum      int
	nodeExecutionTimeout time.Duration

	store             WorkflowStore
	readyCh           chan *readyNodeContext
	eventCh           chan *WorkflowEvent
	subscriberMap     *subscriberMap
	callbackByEventID map[string]func(context.Context, any) error

	// TODO: seperate locks for workflow and workflow runs.
	mu sync.Mutex
}

type readyNodeContext struct {
	NodeID      string
	WorkflowRun *WorkflowRun
}

type WorkflowRun struct {
	ID   string
	Pool *VariablePool
}

type WorkflowOption func(*Workflow)

type subscriberMap struct {
	// data maps subscriberID (workflowRunID + nodeID) to eventIDs.
	data  map[string]map[string]struct{}
	store WorkflowStore
}

func WithLogger(logger *slog.Logger) WorkflowOption {
	return func(w *Workflow) {
		w.logger = logger
	}
}

func WithNodeExecutorNum(executorNum int) WorkflowOption {
	return func(w *Workflow) {
		w.nodeExecutorNum = executorNum
	}
}

func WithNodeExecutionTimeout(duration time.Duration) WorkflowOption {
	return func(w *Workflow) {
		w.nodeExecutionTimeout = duration
	}
}

func NewWorkflow(name string, configs map[string]*NodeConfig, store WorkflowStore, opts ...WorkflowOption) (*Workflow, error) {
	if err := setupInDegree(configs); err != nil {
		return nil, err
	}

	w := &Workflow{
		Name:                 name,
		nodeConfigs:          configs,
		nodeExecutionTimeout: defaultNodeExecutionTimeout,
		store:                store,
		callbackByEventID:    make(map[string]func(context.Context, any) error),
		logger:               slog.Default(),
		nodeExecutorNum:      defaultNodeExecutorNum,
		subscriberMap:        newSubscriberMap(store),
		readyCh:              make(chan *readyNodeContext, readyChannelCapacity),
		eventCh:              make(chan *WorkflowEvent, workflowEventChannelCapacity),
	}

	for _, opt := range opts {
		opt(w)
	}

	return w, nil
}

func setupInDegree(nodeConfigs map[string]*NodeConfig) error {
	for nodeID, nodeConfig := range nodeConfigs {
		for _, nextNodeID := range nodeConfig.NextNodeIDs {
			nextNodeConfig, ok := nodeConfigs[nextNodeID]
			if !ok {
				return fmt.Errorf("node ID does not exist %q", nodeID)
			}
			nextNodeConfig.InDegree++
		}
	}
	return nil
}

func (w *Workflow) Init(ctx context.Context) error {
	workflowModel, err := w.store.GetOrCreateWorkflow(ctx, w.Name)
	if err != nil {
		return err
	}

	w.subscriberMap.load(workflowModel.subscriberMap)
	w.Name = workflowModel.Name
	w.ID = workflowModel.ID

	return nil
}

func (w *Workflow) RecoverWorkflowRuns(ctx context.Context) error {
	workflowRunModels, err := w.store.ListPendingWorkflowRuns(ctx, w.ID, len(w.nodeConfigs))
	if err != nil {
		return err
	}

	for _, workflowRunModel := range workflowRunModels {
		workflowRun, err := w.convertToWorkflowRun(workflowRunModel)
		if err != nil {
			w.logger.ErrorContext(ctx, "failed to recover workflow run", slog.Any("err", err))
			continue
		}
		readyNodeIDs, err := getReadyNodeIDs(w.nodeConfigs, workflowRunModel.NodeExecutions)
		if err != nil {
			return err
		}
		for _, nodeID := range readyNodeIDs {
			w.readyCh <- &readyNodeContext{
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
				nodeStatus, err := w.execute(ctx, nodeContext.WorkflowRun, nodeContext.NodeID)
				if err != nil {
					w.logger.ErrorContext(ctx, "failed to execute node", slog.Any("node_id", nodeContext.NodeID), slog.Any("err", err))
					continue
				}

				if nodeStatus != NodeStatusCompleted {
					continue
				}

				w.mu.Lock()
				if err := w.schedule(ctx, nodeContext.NodeID, nodeContext.WorkflowRun); err != nil {
					w.logger.ErrorContext(ctx, "failed to schedule node", slog.Any("err", err))
				}
				w.mu.Unlock()

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

func (w *Workflow) StartEventProcessor(ctx context.Context) error {
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

func (w *Workflow) RegisterCallback(eventID string, callback func(context.Context, any) error) error {
	if _, ok := w.callbackByEventID[eventID]; ok {
		return fmt.Errorf("event %s already registered", eventID)
	}

	w.callbackByEventID[eventID] = callback

	return nil
}

func (w *Workflow) Post(ctx context.Context, event *WorkflowEvent) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case w.eventCh <- event:
		return nil
	}
}

// TODO: 提供变量池初始化参数而不是让调用者直接操纵 pool.
func (w *Workflow) Run(ctx context.Context, initPoolFunc func(pool *VariablePool)) (*WorkflowRun, error) {
	p := NewVariablePool(w.nodeConfigs)
	initPoolFunc(p)

	data, err := p.Dump()
	if err != nil {
		return nil, err
	}

	nodeExecutions := make(map[string]NodeExecution)
	for nodeID := range w.nodeConfigs {
		nodeExecutions[nodeID] = NodeExecution{
			Status: NodeStatusPending,
		}
	}
	var workflowRunModel *WorkflowRunModel
	if err := w.store.Transaction(ctx, func(ctx context.Context) error {
		var err error
		workflowRunModel, err = w.store.CreateWorkflowRun(ctx, &WorkflowRunModel{
			WorkflowID:     w.ID,
			Data:           data,
			NodeExecutions: nodeExecutions,
		})
		return err
	}); err != nil {
		return nil, err
	}

	workflowRun, err := w.convertToWorkflowRun(workflowRunModel)
	if err != nil {
		return nil, err
	}

	w.readyCh <- &readyNodeContext{
		NodeID:      RootNodeID,
		WorkflowRun: workflowRun,
	}

	return workflowRun, nil
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
		ID:   workflowRunModel.ID,
		Pool: pool,
	}, nil
}

func (w *Workflow) handleWorkflowEvent(ctx context.Context, event *WorkflowEvent) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	match, err := w.subscriberMap.match(event.WorkflowRunID, event.NodeID, event.ID)
	if err != nil {
		return err
	}
	if !match {
		return nil
	}

	callback, err := w.getRegisteredCallback(event.ID)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to get registered callback", slog.Any("err", err))
		return nil
	}

	workflowRunModel, err := w.store.GetWorkflowRun(ctx, event.WorkflowRunID)
	if err != nil {
		return err
	}

	execution, ok := workflowRunModel.NodeExecutions[event.NodeID]
	if !ok {
		return errors.New("failed to get node status")
	}

	workflowRun, err := w.convertToWorkflowRun(workflowRunModel)
	if err != nil {
		return err
	}

	ctx = withNodeStatus(ctx, &execution.Status)
	ctx = withNodeID(ctx, event.NodeID)
	ctx = withWorkflowPool(ctx, workflowRun.Pool)
	var execErr error
	if err := callback(ctx, event.Payload); err != nil {
		execErr = err
		execution.Status = NodeStatusFailed
	}

	switch execution.Status {
	case NodeStatusPause:
		return nil
	case NodeStatusCompleted,
		NodeStatusFailed,
		NodeStatusSkipped:

	default:
		return fmt.Errorf("invalid node status %q after resuming", execution.Status)
	}

	if err := w.subscriberMap.unset(ctx, workflowRun.ID, event.NodeID, event.ID); err != nil {
		return err
	}

	if err := w.store.Transaction(ctx, func(ctx context.Context) error {
		return w.store.UpdateWorkflowRun(ctx, UpdateWorkflowRunInput{
			WorkflowRunID: workflowRun.ID,
			NodeID:        &event.NodeID,
			Execution: &NodeExecution{
				Status: execution.Status,
				Err:    execErr,
			},
		})
	}); err != nil {
		return fmt.Errorf("failed to resume: %w", err)
	}

	if execution.Status == NodeStatusSkipped {
		return nil
	}

	return w.schedule(ctx, event.NodeID, workflowRun)
}

// TODO: scheduler.
func (w *Workflow) schedule(ctx context.Context, nodeID string, workflowRun *WorkflowRun) error {
	nodeConfig, ok := w.nodeConfigs[nodeID]
	if !ok {
		return fmt.Errorf("failed to get node config for %q", nodeID)
	}
	for _, nextNodeID := range nodeConfig.NextNodeIDs {
		ready, err := w.updateAndCheckReadyInDegree(ctx, workflowRun.ID, nextNodeID)
		if err != nil {
			return err
		}
		if !ready {
			continue
		}
		w.readyCh <- &readyNodeContext{
			WorkflowRun: workflowRun,
			NodeID:      nextNodeID,
		}
	}
	return nil
}

// updateAndCheckReadyInDegree return true if the node is ready to be executed.
func (w *Workflow) updateAndCheckReadyInDegree(ctx context.Context, workflowRunID, nodeID string) (bool, error) {
	run, err := w.store.GetWorkflowRun(ctx, workflowRunID)
	if err != nil {
		return false, err
	}
	exec, ok := run.NodeExecutions[nodeID]
	if !ok {
		return false, fmt.Errorf("failed to get execution for %q", nodeID)
	}
	exec.ReadyInDegree++
	if err := w.store.UpdateWorkflowRun(ctx, UpdateWorkflowRunInput{
		WorkflowRunID: workflowRunID,
		NodeID:        &nodeID,
		Execution:     &exec,
	}); err != nil {
		return false, err
	}

	nodeConfig, ok := w.nodeConfigs[nodeID]
	if !ok {
		return false, fmt.Errorf("failed to get node config for %q", nodeID)
	}
	if exec.ReadyInDegree < nodeConfig.InDegree {
		return false, nil
	}

	return true, nil
}

func (w *Workflow) getRegisteredCallback(eventID string) (func(context.Context, any) error, error) {
	callback, ok := w.callbackByEventID[eventID]
	if !ok {
		return nil, fmt.Errorf("callback name not found for event: %q", eventID)
	}

	return callback, nil
}

func (w *Workflow) execute(ctx context.Context, workflowRun *WorkflowRun, nodeID string) (NodeStatus, error) {
	// TODO: distinguish between node types and node IDs.
	act, ok := Lookup(nodeID)
	if !ok {
		return NodeStatusPending, fmt.Errorf("node type %q not registered", nodeID)
	}

	nodeStatus := NodeStatusPending
	eventID := new(string)
	ctx = withNodeID(ctx, nodeID)
	ctx = withWorkflowPool(ctx, workflowRun.Pool)
	ctx = withNodeStatus(ctx, &nodeStatus)
	ctx = withEventID(ctx, eventID)

	ctx, cancel := context.WithTimeout(ctx, w.nodeExecutionTimeout)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- act.Execute(ctx)
	}()
	var execErr error
	select {
	case err := <-done:
		if err != nil {
			nodeStatus = NodeStatusFailed
			execErr = err
		}
	case <-ctx.Done():
		return NodeStatusPending, ctx.Err()
	}

	switch nodeStatus {
	case NodeStatusPause:
		if err := w.handleNodeStatusPause(ctx, workflowRun, nodeID, eventID); err != nil {
			return nodeStatus, err
		}

	case NodeStatusPending:
		nodeStatus = NodeStatusCompleted

	case NodeStatusFailed,
		NodeStatusSkipped:

	default:
		return nodeStatus, fmt.Errorf("node %q entered invalid status %q", nodeID, nodeStatus)
	}

	bytes, err := workflowRun.Pool.Dump()
	if err != nil {
		return nodeStatus, err
	}
	nodeExecution := NodeExecution{
		Status: nodeStatus,
		Err:    execErr,
	}
	if err := w.store.Transaction(ctx, func(ctx context.Context) error {
		return w.store.UpdateWorkflowRun(ctx, UpdateWorkflowRunInput{
			WorkflowRunID: workflowRun.ID,
			NodeID:        &nodeID,
			Execution:     &nodeExecution,
			Data:          &bytes,
		})
	}); err != nil {
		return nodeStatus, err
	}

	return nodeStatus, nil
}

func (w *Workflow) handleNodeStatusPause(ctx context.Context, workflowRun *WorkflowRun, nodeID string, eventID *string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if eventID == nil {
		return fmt.Errorf("node %q paused with nil event ID", nodeID)
	}

	return w.subscriberMap.set(ctx, workflowRun.ID, nodeID, *eventID)
}

func newSubscriberMap(store WorkflowStore) *subscriberMap {
	return &subscriberMap{
		data:  make(map[string]map[string]struct{}),
		store: store,
	}
}

func (m *subscriberMap) load(data map[string]map[string]struct{}) {
	m.data = data
}

func (m *subscriberMap) set(ctx context.Context, workflowRunID string, nodeID string, eventID string) error {
	subscriberID := buildSubscriberID(workflowRunID, nodeID)

	if _, ok := m.data[subscriberID]; ok {
		return fmt.Errorf("subscriber %s already subscribed", subscriberID)
	}

	if _, ok := m.data[subscriberID]; !ok {
		m.data[subscriberID] = make(map[string]struct{})
	}
	m.data[subscriberID][eventID] = struct{}{}

	return m.store.Transaction(ctx, func(ctx context.Context) error {
		return m.store.UpdateWorkflow(ctx, workflowRunID, m.data)
	})
}

func (m *subscriberMap) unset(ctx context.Context, workflowRunID string, nodeID string, eventID string) error {
	subscriberID := buildSubscriberID(workflowRunID, nodeID)
	if _, ok := m.data[subscriberID]; !ok {
		return fmt.Errorf("subscriber %s not found", subscriberID)
	}
	delete(m.data[subscriberID], eventID)

	return m.store.Transaction(ctx, func(ctx context.Context) error {
		return m.store.UpdateWorkflow(ctx, workflowRunID, m.data)
	})
}

func (m *subscriberMap) match(
	targetWorkflowID,
	targetNodeID,
	targetEventID string,
) (bool, error) {
	for subscriberID, eventIDs := range m.data {
		workflowRunID, nodeID, err := parseSubscriberID(subscriberID)
		if err != nil {
			return false, err
		}
		if (workflowRunID != targetWorkflowID) || (nodeID != targetNodeID) {
			continue
		}

		if _, ok := eventIDs[targetEventID]; !ok {
			continue
		}

		return true, nil
	}

	return false, nil
}

func getReadyNodeIDs(configs map[string]*NodeConfig, executions map[string]NodeExecution) ([]string, error) {
	readyNodeIDs := make([]string, 0)

	var dfs func(string) error
	dfs = func(nodeID string) error {
		execution, ok := executions[nodeID]
		if !ok {
			return fmt.Errorf("failed to get status for %q", nodeID)
		}

		status := execution.Status
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
			return nil

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
