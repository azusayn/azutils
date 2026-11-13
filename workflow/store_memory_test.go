package workflow

import (
	"context"
	"errors"
	"testing"
	"time"
)

const (
	testWaitNodeID  = "wait_approval"
	testSkipNodeID  = "skip_after_2s"
	testCheckNodeID = "check_approval"
	testApprovalEvt = "approval_event"
)

type waitApprovalNode struct{}

func (waitApprovalNode) Execute(ctx context.Context) error {
	return WaitFor(ctx, testApprovalEvt)
}

type skipAfter2sNode struct{}

func (skipAfter2sNode) Execute(ctx context.Context) error {
	time.Sleep(2 * time.Second)
	return Skip(ctx)
}

type checkApprovalNode struct{}

func (checkApprovalNode) Execute(ctx context.Context) error {
	time.Sleep(1 * time.Second)
	approved, err := Get[bool](ctx, "approval")
	if err != nil {
		return err
	}
	if !approved {
		return errors.New("approval is false")
	}
	return nil
}

func TestMemoryStoreWorkflowForkResume(t *testing.T) {
	Register(testWaitNodeID, waitApprovalNode{})
	Register(testSkipNodeID, skipAfter2sNode{})
	Register(testCheckNodeID, checkApprovalNode{})

	store := NewMemoryStore()
	configs := map[string]*NodeConfig{
		RootNodeID: {
			NextNodeIDs: []string{testWaitNodeID},
		},
		testWaitNodeID: {
			NextNodeIDs: []string{testSkipNodeID, testCheckNodeID},
		},
		testSkipNodeID: {},
		testCheckNodeID: {
			Inputs: NodeInputConfig{
				"approval": testWaitNodeID,
			},
		},
	}

	w, err := NewWorkflow("memory-store-test", configs, store, WithNodeExecutorNum(2))
	if err != nil {
		t.Fatalf("NewWorkflow: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := w.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}

	go func() {
		if err := w.StartNodeExecutors(ctx); err != nil && !errors.Is(err, context.Canceled) {
			t.Logf("StartNodeExecutors: %v", err)
		}
	}()
	go func() {
		if err := w.StartEventProcessor(ctx); err != nil && !errors.Is(err, context.Canceled) {
			t.Logf("StartEventPoller: %v", err)
		}
	}()

	if err := w.RegisterCallback(testApprovalEvt, func(ctx context.Context, _ any) error {
		if err := Set(ctx, testWaitNodeID, "approval", true); err != nil {
			return err
		}
		return Resume(ctx)
	}); err != nil {
		t.Fatalf("RegisterCallback: %v", err)
	}

	run, err := w.Run(ctx, func(_ *VariablePool) {})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	go func() {
		time.Sleep(200 * time.Millisecond)
		w.eventCh <- &WorkflowEvent{
			ID:            testApprovalEvt,
			WorkflowRunID: run.ID,
			NodeID:        testWaitNodeID,
		}
	}()

	deadline := time.After(12 * time.Second)
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for workflow run: %v", ctx.Err())
		case <-deadline:
			t.Fatal("timed out waiting for leaf nodes to finish")
		default:
		}

		model, err := store.GetWorkflowRun(ctx, run.ID)
		if err != nil {
			t.Fatalf("GetWorkflowRun: %v", err)
		}

		skipExec, skipOK := model.NodeExecutions[testSkipNodeID]
		checkExec, checkOK := model.NodeExecutions[testCheckNodeID]
		if skipOK && checkOK && isTerminalStatus(skipExec.Status) && isTerminalStatus(checkExec.Status) {
			t.Logf("NodeExecutions before assert: %+v (CompletedNodes=%d)", model.NodeExecutions, model.CompletedNodes)

			waitExec := model.NodeExecutions[testWaitNodeID]
			if waitExec.Status != NodeStatusCompleted {
				t.Fatalf("wait node status = %q, want %q", waitExec.Status, NodeStatusCompleted)
			}
			if skipExec.Status != NodeStatusSkipped {
				t.Fatalf("skip node status = %q, want %q", skipExec.Status, NodeStatusSkipped)
			}
			if checkExec.Status != NodeStatusCompleted {
				t.Fatalf("check node status = %q, want %q", checkExec.Status, NodeStatusCompleted)
			}
			if checkExec.Err != nil {
				t.Fatalf("check node error: %v", checkExec.Err)
			}
			return
		}

		time.Sleep(50 * time.Millisecond)
	}
}

func isTerminalStatus(status NodeStatus) bool {
	switch status {
	case NodeStatusCompleted, NodeStatusFailed, NodeStatusSkipped:
		return true
	default:
		return false
	}
}

const (
	faninFailNodeID  = "fanin_fail"
	faninOKNodeID    = "fanin_ok"
	faninSleepNodeID = "fanin_sleep"
	faninWaitNodeID  = "fanin_wait"
	faninSumNodeID   = "fanin_sum"
	faninWaitEvent   = "fanin_wait_event"
	faninExpectedSum = 7
)

type faninFailNode struct{}

func (faninFailNode) Execute(context.Context) error {
	return errors.New("fanin fail node")
}

type faninOKNode struct{}

func (faninOKNode) Execute(ctx context.Context) error {
	return Set(ctx, faninOKNodeID, "a", 1)
}

type faninSleepNode struct{}

func (faninSleepNode) Execute(ctx context.Context) error {
	time.Sleep(300 * time.Millisecond)
	return Set(ctx, faninSleepNodeID, "b", 2)
}

type faninWaitNode struct{}

func (faninWaitNode) Execute(ctx context.Context) error {
	// Set before pause so any WorkflowRun that tips the join still sees c on the shared pool / dumped data.
	if err := Set(ctx, faninWaitNodeID, "c", 4); err != nil {
		return err
	}
	return WaitFor(ctx, faninWaitEvent)
}

type faninSumNode struct{}

func (faninSumNode) Execute(ctx context.Context) error {
	a, err := getFaninInt(ctx, "a")
	if err != nil {
		return err
	}
	b, err := getFaninInt(ctx, "b")
	if err != nil {
		return err
	}
	c, err := getFaninInt(ctx, "c")
	if err != nil {
		return err
	}
	total := a + b + c
	if total != faninExpectedSum {
		return errors.New("fanin sum mismatch")
	}
	return Set(ctx, faninSumNodeID, "sum", total)
}

// getFaninInt accepts int or float64: pool Dump/Load round-trips numbers as float64.
func getFaninInt(ctx context.Context, key string) (int, error) {
	if v, err := Get[int](ctx, key); err == nil {
		return v, nil
	}
	v, err := Get[float64](ctx, key)
	if err != nil {
		return 0, err
	}
	return int(v), nil
}

func faninConfigs(firstNodeID string) map[string]*NodeConfig {
	return map[string]*NodeConfig{
		RootNodeID: {
			NextNodeIDs: []string{firstNodeID, faninSleepNodeID, faninWaitNodeID},
		},
		firstNodeID: {
			NextNodeIDs: []string{faninSumNodeID},
		},
		faninSleepNodeID: {
			NextNodeIDs: []string{faninSumNodeID},
		},
		faninWaitNodeID: {
			NextNodeIDs: []string{faninSumNodeID},
		},
		faninSumNodeID: {
			Inputs: NodeInputConfig{
				"a": firstNodeID,
				"b": faninSleepNodeID,
				"c": faninWaitNodeID,
			},
		},
	}
}

func startFaninWorkflow(t *testing.T, name string, firstNodeID string, firstNode Node) (*Workflow, *MemoryStore, context.Context, context.CancelFunc, *WorkflowRun) {
	t.Helper()

	Register(firstNodeID, firstNode)
	Register(faninSleepNodeID, faninSleepNode{})
	Register(faninWaitNodeID, faninWaitNode{})
	Register(faninSumNodeID, faninSumNode{})

	store := NewMemoryStore()
	w, err := NewWorkflow(name, faninConfigs(firstNodeID), store, WithNodeExecutorNum(3))
	if err != nil {
		t.Fatalf("NewWorkflow: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	if err := w.Init(ctx); err != nil {
		cancel()
		t.Fatalf("Init: %v", err)
	}

	go func() {
		if err := w.StartNodeExecutors(ctx); err != nil && !errors.Is(err, context.Canceled) {
			t.Logf("StartNodeExecutors: %v", err)
		}
	}()
	go func() {
		if err := w.StartEventProcessor(ctx); err != nil && !errors.Is(err, context.Canceled) {
			t.Logf("StartEventProcessor: %v", err)
		}
	}()

	if err := w.RegisterCallback(faninWaitEvent, func(ctx context.Context, _ any) error {
		return Resume(ctx)
	}); err != nil {
		cancel()
		t.Fatalf("RegisterCallback: %v", err)
	}

	run, err := w.Run(ctx, func(_ *VariablePool) {})
	if err != nil {
		cancel()
		t.Fatalf("Run: %v", err)
	}

	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = w.Post(ctx, &WorkflowEvent{
			ID:            faninWaitEvent,
			WorkflowRunID: run.ID,
			NodeID:        faninWaitNodeID,
		})
	}()

	return w, store, ctx, cancel, run
}

func TestMemoryStoreWorkflowFanInBlockedByFailure(t *testing.T) {
	_, store, ctx, cancel, run := startFaninWorkflow(t, "fanin-blocked", faninFailNodeID, faninFailNode{})
	defer cancel()

	deadline := time.After(12 * time.Second)
	predecessorsDone := false
	stableRounds := 0
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for fan-in predecessors: %v", ctx.Err())
		case <-deadline:
			t.Fatal("timed out waiting for fan-in blocked assertion")
		default:
		}

		model, err := store.GetWorkflowRun(ctx, run.ID)
		if err != nil {
			t.Fatalf("GetWorkflowRun: %v", err)
		}

		failExec := model.NodeExecutions[faninFailNodeID]
		sleepExec := model.NodeExecutions[faninSleepNodeID]
		waitExec := model.NodeExecutions[faninWaitNodeID]
		sumExec := model.NodeExecutions[faninSumNodeID]

		if failExec.Status == NodeStatusFailed &&
			sleepExec.Status == NodeStatusCompleted &&
			waitExec.Status == NodeStatusCompleted {
			predecessorsDone = true
			if sumExec.Status != NodeStatusPending {
				t.Fatalf("sum status = %q, want %q while a predecessor failed", sumExec.Status, NodeStatusPending)
			}
			if sumExec.ReadyInDegree != 2 {
				t.Fatalf("sum ReadyInDegree = %d, want 2", sumExec.ReadyInDegree)
			}
			stableRounds++
			if stableRounds >= 5 {
				t.Logf("NodeExecutions blocked: %+v", model.NodeExecutions)
				return
			}
		} else if predecessorsDone {
			t.Fatalf("predecessors left expected terminal state: fail=%q sleep=%q wait=%q",
				failExec.Status, sleepExec.Status, waitExec.Status)
		}

		time.Sleep(50 * time.Millisecond)
	}
}

func TestMemoryStoreWorkflowFanInSumOnAllSuccess(t *testing.T) {
	_, store, ctx, cancel, run := startFaninWorkflow(t, "fanin-sum", faninOKNodeID, faninOKNode{})
	defer cancel()

	deadline := time.After(12 * time.Second)
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for fan-in sum: %v", ctx.Err())
		case <-deadline:
			t.Fatal("timed out waiting for fan-in sum to complete")
		default:
		}

		model, err := store.GetWorkflowRun(ctx, run.ID)
		if err != nil {
			t.Fatalf("GetWorkflowRun: %v", err)
		}

		okExec := model.NodeExecutions[faninOKNodeID]
		sleepExec := model.NodeExecutions[faninSleepNodeID]
		waitExec := model.NodeExecutions[faninWaitNodeID]
		sumExec := model.NodeExecutions[faninSumNodeID]

		if sumExec.Status == NodeStatusFailed {
			t.Fatalf("sum node failed: %v (executions=%+v)", sumExec.Err, model.NodeExecutions)
		}
		if sumExec.Status == NodeStatusCompleted {
			t.Logf("NodeExecutions sum success: %+v", model.NodeExecutions)
			if okExec.Status != NodeStatusCompleted {
				t.Fatalf("ok node status = %q, want %q", okExec.Status, NodeStatusCompleted)
			}
			if sleepExec.Status != NodeStatusCompleted {
				t.Fatalf("sleep node status = %q, want %q", sleepExec.Status, NodeStatusCompleted)
			}
			if waitExec.Status != NodeStatusCompleted {
				t.Fatalf("wait node status = %q, want %q", waitExec.Status, NodeStatusCompleted)
			}
			if sumExec.Err != nil {
				t.Fatalf("sum node error: %v", sumExec.Err)
			}
			return
		}

		time.Sleep(50 * time.Millisecond)
	}
}

func TestMemoryStoreUpdateWorkflowIsolatesSubscriberMap(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	wfA, err := store.GetOrCreateWorkflow(ctx, "graph-a")
	if err != nil {
		t.Fatalf("GetOrCreateWorkflow A: %v", err)
	}
	wfB, err := store.GetOrCreateWorkflow(ctx, "graph-b")
	if err != nil {
		t.Fatalf("GetOrCreateWorkflow B: %v", err)
	}

	runA, err := store.CreateWorkflowRun(ctx, &WorkflowRunModel{WorkflowID: wfA.ID})
	if err != nil {
		t.Fatalf("CreateWorkflowRun A: %v", err)
	}
	runB, err := store.CreateWorkflowRun(ctx, &WorkflowRunModel{WorkflowID: wfB.ID})
	if err != nil {
		t.Fatalf("CreateWorkflowRun B: %v", err)
	}

	subsA := map[string]map[string]struct{}{
		"runA:node": {"event-a": {}},
	}
	subsB := map[string]map[string]struct{}{
		"runB:node": {"event-b": {}},
	}
	if err := store.UpdateWorkflow(ctx, runA.ID, subsA); err != nil {
		t.Fatalf("UpdateWorkflow A: %v", err)
	}
	if err := store.UpdateWorkflow(ctx, runB.ID, subsB); err != nil {
		t.Fatalf("UpdateWorkflow B: %v", err)
	}

	gotA, err := store.GetOrCreateWorkflow(ctx, "graph-a")
	if err != nil {
		t.Fatalf("reload A: %v", err)
	}
	gotB, err := store.GetOrCreateWorkflow(ctx, "graph-b")
	if err != nil {
		t.Fatalf("reload B: %v", err)
	}

	if _, ok := gotA.subscriberMap["runA:node"]["event-a"]; !ok {
		t.Fatalf("graph-a lost its subscription: %+v", gotA.subscriberMap)
	}
	if _, ok := gotA.subscriberMap["runB:node"]; ok {
		t.Fatalf("graph-a picked up graph-b subscription: %+v", gotA.subscriberMap)
	}
	if _, ok := gotB.subscriberMap["runB:node"]["event-b"]; !ok {
		t.Fatalf("graph-b lost its subscription: %+v", gotB.subscriberMap)
	}
	if _, ok := gotB.subscriberMap["runA:node"]; ok {
		t.Fatalf("graph-b picked up graph-a subscription: %+v", gotB.subscriberMap)
	}
}

func TestMemoryStoreListPendingUnimplemented(t *testing.T) {
	store := NewMemoryStore()
	_, err := store.ListPendingWorkflowRuns(context.Background(), "", 1)
	if err == nil || err.Error() != "unimplemented" {
		t.Fatalf("ListPendingWorkflowRuns error = %v, want unimplemented", err)
	}
}
