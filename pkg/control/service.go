package control

import (
	"context"
	"sync"
	"time"

	clusterpb "cluster/proto"
)

// Service 实现 ControlServiceServer。
//
// 设计：
// - 订阅侧（所有节点）会调用 SubscribeCluster
// - 当前 leader（Master）负责把集群状态事件 broadcast 给所有订阅者
// - 非 leader 节点只维持本地订阅，不主动 broadcast
//
// 目前实现先支持：
// - SubscribeCluster：注册订阅并持续推送事件
// - RequestManualElect：回调到上层（由 main 绑定）触发手工切换
//
// 具体的“哪些事件”“何时 broadcast”由上层在角色变化时调用 broadcaster.Broadcast。
type Service struct {
	clusterpb.UnimplementedControlServiceServer

	clusterID string

	b *ControlBroadcaster

	mu sync.Mutex
	// manualElectHandler 由上层注入（例如调用 election.Client.SetManualSwitch 并触发 leader 重算）。
	manualElectHandler func(ctx context.Context, targetNodeID string) (ok bool, msg string)
}

func NewService(clusterID string, b *ControlBroadcaster) *Service {
	return &Service{clusterID: clusterID, b: b}
}

func (s *Service) SetManualElectHandler(fn func(ctx context.Context, targetNodeID string) (bool, string)) {
	s.mu.Lock()
	s.manualElectHandler = fn
	s.mu.Unlock()
}

func (s *Service) SubscribeCluster(req *clusterpb.SubscribeRequest, stream clusterpb.ControlService_SubscribeClusterServer) error {
	if req.GetClusterId() != s.clusterID {
		return nil
	}

	nodeID := req.GetNodeId()
	ch := s.b.AddSubscriber(nodeID)
	defer s.b.RemoveSubscriber(nodeID)

	ctx := stream.Context()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			if ev.GetClusterId() == "" {
				ev.ClusterId = s.clusterID
			}
			if ev.TimestampUnixMs == 0 {
				ev.TimestampUnixMs = time.Now().UnixMilli()
			}
			if err := stream.Send(ev); err != nil {
				return err
			}
		}
	}
}

func (s *Service) RequestManualElect(ctx context.Context, req *clusterpb.ManualElectRequest) (*clusterpb.ManualElectResponse, error) {
	if req.GetClusterId() != s.clusterID {
		return &clusterpb.ManualElectResponse{Ok: false, Message: "cluster_id 不匹配"}, nil
	}

	s.mu.Lock()
	h := s.manualElectHandler
	s.mu.Unlock()
	if h == nil {
		return &clusterpb.ManualElectResponse{Ok: false, Message: "未配置手工切换处理器"}, nil
	}

	ok, msg := h(ctx, req.GetTargetNodeId())
	return &clusterpb.ManualElectResponse{Ok: ok, Message: msg}, nil
}
