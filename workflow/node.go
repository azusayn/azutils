package workflow

import (
	"context"
	"errors"
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
	status, err := extractNodeStatus(ctx)
	if err != nil {
		return err
	}
	*status = NodeStatusPause

	ctxEventID, err := extractEventID(ctx)
	if err != nil {
		return err
	}
	*ctxEventID = eventID

	return nil
}

func Resume(ctx context.Context) error {
	nodeStatus, err := extractNodeStatus(ctx)
	if err != nil {
		return err
	}

	if *nodeStatus != NodeStatusPause {
		return errors.New("cannot resume unsuspend node")
	}
	*nodeStatus = NodeStatusCompleted

	return nil
}
