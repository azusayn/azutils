package workflow

import (
	"context"
	"errors"
)

type contextKey string

const (
	contextKeyWorkflowRun    contextKey = "workflow_run"
	contextKeyEventID        contextKey = "event_id"
	contextKeyWorkflowPool   contextKey = "pool"
	contextKeyWorkflowNodeID contextKey = "nodeID"
)

func withWorkflowRun(ctx context.Context, workflowRun *WorkflowRun) context.Context {
	return context.WithValue(ctx, contextKeyWorkflowRun, workflowRun)
}

func extractWorkflowRun(ctx context.Context) (*WorkflowRun, error) {
	workflowRun, ok := ctx.Value(contextKeyWorkflowRun).(*WorkflowRun)
	if !ok {
		return nil, errors.New("failed to extract workflow run from context")
	}
	return workflowRun, nil
}

func withEventID(ctx context.Context, eventID string) context.Context {
	return context.WithValue(ctx, contextKeyEventID, eventID)
}

func extractEventID(ctx context.Context) (string, error) {
	eventID, ok := ctx.Value(contextKeyEventID).(string)
	if !ok {
		return "", errors.New("failed to extract event ID from context")
	}

	return eventID, nil
}

func withWorkflowPool(ctx context.Context, workflowPool *VariablePool) context.Context {
	return context.WithValue(ctx, contextKeyWorkflowPool, workflowPool)
}

func extractWorkflowPool(ctx context.Context) (*VariablePool, error) {
	workflowPool, ok := ctx.Value(contextKeyWorkflowPool).(*VariablePool)
	if !ok {
		return nil, errors.New("failed to extract workflow pool from context")
	}
	return workflowPool, nil
}

func withNodeID(ctx context.Context, nodeID string) context.Context {
	return context.WithValue(ctx, contextKeyWorkflowNodeID, nodeID)
}

func extractNodeID(ctx context.Context) (string, error) {
	nodeID, ok := ctx.Value(contextKeyWorkflowNodeID).(string)
	if !ok {
		return "", errors.New("failed to extract node ID from context")
	}
	return nodeID, nil
}
