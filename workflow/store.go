package workflow

import (
	"context"
	"encoding/json"
	"time"

	"github.com/azusayn/azutils/db"
)

type WorkflowRunModel struct {
	ID   string
	Data json.RawMessage
	// NodeExecutions maps node ID to node status.
	NodeExecutions map[string]NodeStatus
	CompletedNodes int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type WorkflowStore interface {
	db.Transaction
	GetWorkflowRun(ctx context.Context, workflowRunID string) (*WorkflowRunModel, error)
	ListPendingWorkflowRuns(ctx context.Context, totalNodes int) ([]*WorkflowRunModel, error)
	CreateWorkflowRun(ctx context.Context, workflowRun *WorkflowRunModel) (*WorkflowRunModel, error)
	UpdateInterestList(ctx context.Context, workflowRunID string, subscriberMap map[string]map[string]struct{}) error
	UpdateWorkflowRunStatus(ctx context.Context, workflowRunID string, singleNodeExecution map[string]NodeStatus, err error, data json.RawMessage) error
	ResumeWrokflowRun(ctx context.Context, workflowRunID, nodeID string) error
}
