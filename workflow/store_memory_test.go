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

func TestMemoryStoreListPendingUnimplemented(t *testing.T) {
	store := NewMemoryStore()
	_, err := store.ListPendingWorkflowRuns(context.Background(), "", 1)
	if err == nil || err.Error() != "unimplemented" {
		t.Fatalf("ListPendingWorkflowRuns error = %v, want unimplemented", err)
	}
}
