package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

var _ WorkflowStore = (*MemoryStore)(nil)

type MemoryStore struct {
	mu        sync.Mutex
	workflows map[string]*WorkflowModel // name -> model
	byID      map[string]*WorkflowModel // id -> model
	runs      map[string]*WorkflowRunModel
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		workflows: make(map[string]*WorkflowModel),
		byID:      make(map[string]*WorkflowModel),
		runs:      make(map[string]*WorkflowRunModel),
	}
}

func (s *MemoryStore) Transaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

func (s *MemoryStore) GetOrCreateWorkflow(_ context.Context, name string) (*WorkflowModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.workflows[name]; ok {
		return cloneWorkflowModel(existing), nil
	}

	model := &WorkflowModel{
		ID:            uuid.NewString(),
		Name:          name,
		subscriberMap: make(map[string]map[string]struct{}),
	}
	s.workflows[name] = model
	s.byID[model.ID] = model
	return cloneWorkflowModel(model), nil
}

func (s *MemoryStore) UpdateWorkflow(_ context.Context, workflowRunID string, subscriberMap map[string]map[string]struct{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	run, ok := s.runs[workflowRunID]
	if !ok {
		return fmt.Errorf("workflow run %q not found", workflowRunID)
	}
	model, ok := s.byID[run.WorkflowID]
	if !ok {
		return fmt.Errorf("workflow %q not found", run.WorkflowID)
	}
	model.subscriberMap = cloneSubscriberMap(subscriberMap)
	return nil
}

func (s *MemoryStore) GetWorkflowRun(_ context.Context, workflowRunID string) (*WorkflowRunModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	run, ok := s.runs[workflowRunID]
	if !ok {
		return nil, fmt.Errorf("workflow run %q not found", workflowRunID)
	}
	return cloneWorkflowRun(run), nil
}

func (s *MemoryStore) ListPendingWorkflowRuns(context.Context, string, int) ([]*WorkflowRunModel, error) {
	return nil, errors.New("unimplemented")
}

func (s *MemoryStore) CreateWorkflowRun(_ context.Context, workflowRun *WorkflowRunModel) (*WorkflowRunModel, error) {
	if workflowRun == nil {
		return nil, errors.New("nil workflow run")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	run := &WorkflowRunModel{
		ID:             workflowRun.ID,
		WorkflowID:     workflowRun.WorkflowID,
		Data:           append(json.RawMessage(nil), workflowRun.Data...),
		NodeExecutions: cloneNodeExecutions(workflowRun.NodeExecutions),
		CompletedNodes: workflowRun.CompletedNodes,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if run.ID == "" {
		run.ID = uuid.NewString()
	}
	if run.NodeExecutions == nil {
		run.NodeExecutions = make(map[string]NodeExecution)
	}
	s.runs[run.ID] = run
	return cloneWorkflowRun(run), nil
}

func (s *MemoryStore) UpdateWorkflowRun(_ context.Context, input UpdateWorkflowRunInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	run, ok := s.runs[input.WorkflowRunID]
	if !ok {
		return fmt.Errorf("workflow run %q not found", input.WorkflowRunID)
	}
	if input.Data != nil {
		run.Data = append(json.RawMessage(nil), (*input.Data)...)
	}
	if input.NodeID != nil && input.Execution != nil {
		run.NodeExecutions[*input.NodeID] = *input.Execution
		run.CompletedNodes = countCompletedNodes(run.NodeExecutions)
	}
	run.UpdatedAt = time.Now()
	return nil
}

func cloneWorkflowModel(src *WorkflowModel) *WorkflowModel {
	if src == nil {
		return nil
	}
	return &WorkflowModel{
		ID:            src.ID,
		Name:          src.Name,
		subscriberMap: cloneSubscriberMap(src.subscriberMap),
	}
}

func cloneSubscriberMap(src map[string]map[string]struct{}) map[string]map[string]struct{} {
	if src == nil {
		return nil
	}
	dst := make(map[string]map[string]struct{}, len(src))
	for subscriberID, events := range src {
		copied := make(map[string]struct{}, len(events))
		for eventID := range events {
			copied[eventID] = struct{}{}
		}
		dst[subscriberID] = copied
	}
	return dst
}

func cloneWorkflowRun(src *WorkflowRunModel) *WorkflowRunModel {
	if src == nil {
		return nil
	}
	return &WorkflowRunModel{
		ID:             src.ID,
		WorkflowID:     src.WorkflowID,
		Data:           append(json.RawMessage(nil), src.Data...),
		NodeExecutions: cloneNodeExecutions(src.NodeExecutions),
		CompletedNodes: src.CompletedNodes,
		CreatedAt:      src.CreatedAt,
		UpdatedAt:      src.UpdatedAt,
	}
}

func cloneNodeExecutions(src map[string]NodeExecution) map[string]NodeExecution {
	if src == nil {
		return nil
	}
	dst := make(map[string]NodeExecution, len(src))
	for id, execution := range src {
		dst[id] = execution
	}
	return dst
}

func countCompletedNodes(executions map[string]NodeExecution) int {
	completed := 0
	for _, execution := range executions {
		switch execution.Status {
		case NodeStatusCompleted, NodeStatusFailed, NodeStatusSkipped:
			completed++
		}
	}
	return completed
}
