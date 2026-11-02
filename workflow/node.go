package workflow

import (
	"context"
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
