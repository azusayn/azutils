package workflow

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

const RootNodeID = "root"

type NodeStatus string

const (
	NodeStatusPending   NodeStatus = "pending"
	NodeStatusFailed    NodeStatus = "failed"
	NodeStatusPause     NodeStatus = "pause"
	NodeStatusSkipped   NodeStatus = "skipped"
	NodeStatusCompleted NodeStatus = "completed"
)

var (
	registryMu sync.Mutex
	registry   = map[string]Node{}
)

type Node interface {
	Execute(ctx context.Context) error
}

func Register(nodeType string, node Node) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[nodeType] = node
}

func Lookup(nodeType string) (Node, bool) {
	registryMu.Lock()
	defer registryMu.Unlock()
	found, ok := registry[nodeType]
	return found, ok
}

func Skip(ctx context.Context) error {
	status, err := extractNodeStatus(ctx)
	if err != nil {
		return err
	}
	*status = NodeStatusSkipped
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
	status, err := extractNodeStatus(ctx)
	if err != nil {
		return err
	}

	*status = NodeStatusPause

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
	nodeStatus, err := extractNodeStatus(ctx)
	if err != nil {
		return err
	}

	if *nodeStatus != NodeStatusPause {
		return errors.New("cannot resume unsuspend node")
	}
	workflowRun.subscriberMap.unset(ctx, workflowRun.ID, nodeID, eventID)
	*nodeStatus = NodeStatusCompleted

	if err := workflowRun.store.ResumeWrokflowRun(ctx, workflowRun.ID, nodeID); err != nil {
		return fmt.Errorf("failed to resume: %w", err)
	}

	// TODO: scheduler.
	nodeConfig, ok := workflowRun.nodeConfigs[nodeID]
	if !ok {
		return fmt.Errorf("failed to get node config for %q", nodeID)
	}
	for _, nextNodeID := range nodeConfig.NextNodeIDs {
		workflowRun.readyCh <- &ReadyNodeContext{
			WorkflowRun: workflowRun,
			NodeID:      nextNodeID,
		}
	}

	return nil
}
