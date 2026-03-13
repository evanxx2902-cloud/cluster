package election

import (
	"sync"
	"time"

	clusterpb "cluster/proto"
)

// ObservedState 记录从某个节点观测到的状态 + 最近心跳时间。
type ObservedState struct {
	State    *clusterpb.NodeState
	LastSeen time.Time
}

// Table 保存对集群节点的观测视图。
type Table struct {
	mu sync.RWMutex

	byNodeID map[string]ObservedState
}

func NewTable() *Table {
	return &Table{byNodeID: make(map[string]ObservedState)}
}

func (t *Table) Update(now time.Time, st *clusterpb.NodeState) {
	t.mu.Lock()
	t.byNodeID[st.GetNodeId()] = ObservedState{State: st, LastSeen: now}
	t.mu.Unlock()
}

func (t *Table) Snapshot() map[string]ObservedState {
	t.mu.RLock()
	defer t.mu.RUnlock()

	cp := make(map[string]ObservedState, len(t.byNodeID))
	for k, v := range t.byNodeID {
		cp[k] = v
	}
	return cp
}
