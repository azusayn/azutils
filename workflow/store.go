package workflow

import (
	"context"
	"encoding/json"
	"time"

	"github.com/azusayn/azutils/db"
)

type WorkflowModel struct {
	ID            string
	Name          string
	subscriberMap map[string]map[string]struct{}
}

type WorkflowRunModel struct {
	ID         string
	WorkflowID string
	Data       json.RawMessage
	// NodeExecutions maps node ID to node status.
	NodeExecutions map[string]NodeExecution
	CompletedNodes int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type NodeExecution struct {
	Err    error
	Status NodeStatus
}

type UpdateWorkflowRunInput struct {
	WorkflowRunID string
	NodeID        *string
	Execution     *NodeExecution
	Data          *json.RawMessage
}

type WorkflowStore interface {
	db.Transaction
	// GetOrCreateWorkflow creates a new workflow or returns the existing one if it already exists.
	GetOrCreateWorkflow(ctx context.Context, name string) (*WorkflowModel, error)
	UpdateWorkflow(ctx context.Context, workflowRunID string, subscriberMap map[string]map[string]struct{}) error

	GetWorkflowRun(ctx context.Context, workflowRunID string) (*WorkflowRunModel, error)
	ListPendingWorkflowRuns(ctx context.Context, workflowID string, totalNodes int) ([]*WorkflowRunModel, error)
	CreateWorkflowRun(ctx context.Context, workflowRun *WorkflowRunModel) (*WorkflowRunModel, error)
	UpdateWorkflowRun(ctx context.Context, input UpdateWorkflowRunInput) error
}
