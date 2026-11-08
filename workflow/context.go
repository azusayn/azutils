package workflow

import (
	"context"
	"errors"
	"fmt"
)

type contextKey string

const (
	contextKeyEventID        contextKey = "event_id"
	contextKeyWorkflowPool   contextKey = "pool"
	contextKeyWorkflowNodeID contextKey = "node_id"
	contextKeyNodeStatus     contextKey = "node_status"
)

func withEventID(ctx context.Context, eventID *string) context.Context {
	return context.WithValue(ctx, contextKeyEventID, eventID)
}

func extractEventID(ctx context.Context) (*string, error) {
	eventID, ok := ctx.Value(contextKeyEventID).(*string)
	if !ok || eventID == nil {
		return nil, errors.New("failed to extract event ID from context")
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

func withNodeStatus(ctx context.Context, status *NodeStatus) context.Context {
	return context.WithValue(ctx, contextKeyNodeStatus, status)
}

func extractNodeStatus(ctx context.Context) (*NodeStatus, error) {
	status, ok := ctx.Value(contextKeyNodeStatus).(*NodeStatus)
	if !ok || status == nil {
		return nil, fmt.Errorf("failed to extract node status from context")
	}
	return status, nil
}
