package workflow

import (
	"context"
	"encoding/json"
	"time"
)

type WorkflowRunModel struct {
	ID                string          `orm:"id" json:"id"`
	Data              json.RawMessage `orm:"data" json:"data"`
	InterestList      string          `orm:"interest_list" json:"interestList"`
	CurrentNodeID     string          `orm:"current_node_id" json:"currentNodeId"`
	CurrentNodeStatus NodeStatus      `orm:"current_node_status" json:"currentNodeStatus"`
	CreatedAt         time.Time       `orm:"created_at" json:"createdAt"`
	UpdatedAt         time.Time       `orm:"updated_at" json:"updatedAt"`
}

type WorkflowStore interface {
	GetWorkflowRun(ctx context.Context, workflowRunID string) (*WorkflowRunModel, error)
	CreateWorkflowRun(ctx context.Context) (*WorkflowRunModel, error)
	UpdateInterestList(ctx context.Context, workflowRunID string, subscriberMap map[string]map[string]struct{}) error
	ResumeWrokflowRun(ctx context.Context, workflowRunID string) error
	UpdateWorkflowRunStatus(ctx context.Context, workflowRunID string, currentNodeID string, currentNodeStatus NodeStatus, err error, data json.RawMessage) error
}
