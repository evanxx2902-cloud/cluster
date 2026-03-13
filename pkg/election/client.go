package election

import (
	"context"
	"fmt"
	"sync"
	"time"

	clusterpb "cluster/proto"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

// DialFunc 用于获取到指定 addr 的 gRPC 连接。
//
// 设计为函数注入，避免 election 包依赖具体的连接管理实现。
type DialFunc func(ctx context.Context, addr string) (*grpc.ClientConn, error)

type Peer struct {
	NodeID string
	Addr   string
}

// Client 负责与其他主备节点进行选举心跳交换。
//
// 这是“无多数派”的 VRRP 类算法实现：
// - 通过心跳交换 NodeState
// - 每个节点本地计算 leader
// - 通过确定性比较规则尽量减少双 Master
//
// 注意：网络分区仍可能产生双 Master。
type Client struct {
	clusterID string
	selfID    string
	selfAddr  string

	priority int32

	heartbeatInterval time.Duration
	electionTimeout   time.Duration

	mu sync.Mutex
	// self 代表本节点对外广播的状态（role/term/switchType 等）。
	self *clusterpb.NodeState

	// 当前本节点认为的 leader
	leaderID string

	observed *Table
}

func NewClient(clusterID, selfID, selfAddr string, priority int32, hbInterval, electTimeout time.Duration, role clusterpb.NodeRole) *Client {
	now := time.Now()
	self := &clusterpb.NodeState{
		ClusterId:      clusterID,
		NodeId:         selfID,
		Addr:           selfAddr,
		Role:           role,
		Term:           1,
		Priority:       priority,
		SwitchType:     clusterpb.SwitchType_SWITCH_TYPE_TIMEOUT,
		LastSeenUnixMs: now.UnixMilli(),
	}

	c := &Client{
		clusterID:         clusterID,
		selfID:            selfID,
		selfAddr:          selfAddr,
		priority:          priority,
		heartbeatInterval: hbInterval,
		electionTimeout:   electTimeout,
		self:              self,
		leaderID:          "",
		observed:          NewTable(),
	}

	c.observed.Update(now, c.Self())
	return c
}

func (c *Client) Self() *clusterpb.NodeState {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := *c.self
	return &cp
}

func (c *Client) LeaderID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.leaderID
}

func (c *Client) setLeaderID(id string) {
	c.mu.Lock()
	c.leaderID = id
	c.mu.Unlock()
}

// PromoteToMaster 将本节点晋升为 Master（仅当从 Backup 变为 Master 时 term++）。
func (c *Client) PromoteToMaster(now time.Time) {
	c.mu.Lock()
	if c.self.Role != clusterpb.NodeRole_NODE_ROLE_MASTER {
		c.self.Term += 1
		c.self.Role = clusterpb.NodeRole_NODE_ROLE_MASTER
	}
	c.self.SwitchType = clusterpb.SwitchType_SWITCH_TYPE_TIMEOUT
	c.self.LastSeenUnixMs = now.UnixMilli()
	c.mu.Unlock()

	c.observed.Update(now, c.Self())
}

func (c *Client) DemoteToBackup(now time.Time) {
	c.mu.Lock()
	if c.self.Role != clusterpb.NodeRole_NODE_ROLE_SLAVE {
		c.self.Role = clusterpb.NodeRole_NODE_ROLE_BACKUP
		c.self.SwitchType = clusterpb.SwitchType_SWITCH_TYPE_TIMEOUT
	}
	c.self.LastSeenUnixMs = now.UnixMilli()
	c.mu.Unlock()

	c.observed.Update(now, c.Self())
}

// SetManualSwitch 将 switch_type 设置为 MANUAL（用于手工指定）。
func (c *Client) SetManualSwitch(now time.Time) {
	c.mu.Lock()
	c.self.SwitchType = clusterpb.SwitchType_SWITCH_TYPE_MANUAL
	c.self.LastSeenUnixMs = now.UnixMilli()
	c.mu.Unlock()

	c.observed.Update(now, c.Self())
}

// ObserveFromHeartbeat 更新观测表。
func (c *Client) ObserveFromHeartbeat(now time.Time, hb *clusterpb.ElectionHeartbeat) {
	st := hb.GetFrom()
	if st == nil || st.GetClusterId() != c.clusterID {
		return
	}

	// 补上 last_seen，避免依赖对方填。
	cp := *st
	cp.LastSeenUnixMs = now.UnixMilli()
	c.observed.Update(now, &cp)
}

// PickLeaderFromObserved 基于观测表选出 leader。
func (c *Client) PickLeaderFromObserved(now time.Time) (*clusterpb.NodeState, bool) {
	snap := c.observed.Snapshot()
	cands := make([]Candidate, 0, len(snap)+1)

	// 先把自己也作为候选（只要不是 Slave）。
	self := c.Self()
	if self.Role != clusterpb.NodeRole_NODE_ROLE_SLAVE {
		cands = append(cands, CandidateFromState(self))
	}

	for _, obs := range snap {
		st := obs.State
		if st == nil {
			continue
		}
		// 只让 Master/Backup 参与选举。
		if st.Role == clusterpb.NodeRole_NODE_ROLE_SLAVE {
			continue
		}
		// 过滤超时节点。
		if now.Sub(obs.LastSeen) > c.electionTimeout {
			continue
		}
		cands = append(cands, CandidateFromState(st))
	}

	winner, ok := PickLeader(cands)
	if !ok {
		return nil, false
	}

	if winner.NodeID == self.NodeId {
		return self, true
	}
	for _, obs := range snap {
		if obs.State != nil && obs.State.NodeId == winner.NodeID {
			return obs.State, true
		}
	}
	return nil, false
}

// Evaluate 根据观测到的状态计算 leader，并调整自身角色。
func (c *Client) Evaluate(now time.Time) (*clusterpb.NodeState, bool) {
	leader, ok := c.PickLeaderFromObserved(now)
	if !ok {
		return nil, false
	}

	c.setLeaderID(leader.GetNodeId())

	if leader.GetNodeId() == c.selfID {
		c.PromoteToMaster(now)
	} else {
		c.DemoteToBackup(now)
	}

	return leader, true
}

// Run 在后台维护对 peers 的心跳流，并周期性执行 Evaluate。
func (c *Client) Run(ctx context.Context, dial DialFunc, peers []Peer) error {
	g, ctx := errgroup.WithContext(ctx)

	// 心跳连接
	for _, p := range peers {
		if p.NodeID == "" || p.NodeID == c.selfID {
			continue
		}
		p := p
		g.Go(func() error {
			return c.runPeerLoop(ctx, dial, p)
		})
	}

	// 选举评估 loop（即使没有 peer，也会基于自身信息产生 leader）
	g.Go(func() error {
		ticker := time.NewTicker(c.heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				c.Evaluate(time.Now())
			}
		}
	})

	return g.Wait()
}

func (c *Client) runPeerLoop(ctx context.Context, dial DialFunc, p Peer) error {
	backoff := 200 * time.Millisecond
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		cc, err := dial(ctx, p.Addr)
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				if backoff < 2*time.Second {
					backoff *= 2
				}
				continue
			}
		}

		backoff = 200 * time.Millisecond
		if err := c.RunHeartbeatStream(ctx, p.NodeID, cc); err != nil {
			// 断线/错误：短暂退避后重连。
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				continue
			}
		}
	}
}

// RunHeartbeatStream 与目标节点维持一条 Heartbeat 双向流。
func (c *Client) RunHeartbeatStream(ctx context.Context, nodeID string, cc *grpc.ClientConn) error {
	cli := clusterpb.NewElectionServiceClient(cc)
	stream, err := cli.Heartbeat(ctx)
	if err != nil {
		return fmt.Errorf("建立心跳流失败 node=%s: %w", nodeID, err)
	}

	errCh := make(chan error, 2)

	// 接收
	go func() {
		for {
			hb, err := stream.Recv()
			if err != nil {
				errCh <- err
				return
			}
			c.ObserveFromHeartbeat(time.Now(), hb)
		}
	}()

	// 发送
	go func() {
		ticker := time.NewTicker(c.heartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			case <-ticker.C:
				self := c.Self()
				hb := &clusterpb.ElectionHeartbeat{
					ClusterId:       self.ClusterId,
					From:            self,
					TimestampUnixMs: time.Now().UnixMilli(),
				}
				if err := stream.Send(hb); err != nil {
					errCh <- err
					return
				}
			}
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}
