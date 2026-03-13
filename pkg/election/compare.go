package election

import (
	"sort"

	clusterpb "cluster/proto"
)

// Candidate 表示参与 leader 比较的节点（仅 Master/Backup）。
type Candidate struct {
	NodeID     string
	Addr       string
	Priority   int32
	Term       uint64
	SwitchType clusterpb.SwitchType
}

func CandidateFromState(st *clusterpb.NodeState) Candidate {
	return Candidate{
		NodeID:     st.GetNodeId(),
		Addr:       st.GetAddr(),
		Priority:   st.GetPriority(),
		Term:       st.GetTerm(),
		SwitchType: st.GetSwitchType(),
	}
}

func (c Candidate) Less(other Candidate) bool {
	// 返回 true 表示 c 比 other “更弱”（排序时用）。
	// 我们希望最终选出“更强”的作为 leader。

	// 1) switch_type：MANUAL(2) > FAILOVER(1) > TIMEOUT(0)
	if c.SwitchType != other.SwitchType {
		return c.SwitchType < other.SwitchType
	}
	// 2) term：越大越新
	if c.Term != other.Term {
		return c.Term < other.Term
	}
	// 3) priority：越大越优
	if c.Priority != other.Priority {
		return c.Priority < other.Priority
	}
	// 4) node_id：字典序，确保确定性
	return c.NodeID > other.NodeID
}

// PickLeader 从候选集合中挑选唯一 leader。
func PickLeader(candidates []Candidate) (Candidate, bool) {
	if len(candidates) == 0 {
		return Candidate{}, false
	}

	sorted := append([]Candidate(nil), candidates...)
	sort.Slice(sorted, func(i, j int) bool {
		// true 表示 i 应排在 j 前面，这里把“更强”的排前面。
		return sorted[j].Less(sorted[i])
	})

	return sorted[0], true
}
