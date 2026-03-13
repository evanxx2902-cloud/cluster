package control

import (
	"context"
	"fmt"

	clusterpb "cluster/proto"
)

func (s *Service) StartRemoteListen(ctx context.Context, req *clusterpb.RemoteListenRequest) (*clusterpb.RemoteListenResponse, error) {
	// 先把 RPC 打通；具体实现会在 tunnel 实现后接上。
	if req.GetClusterId() != s.clusterID {
		return &clusterpb.RemoteListenResponse{Ok: false, Message: "cluster_id 不匹配"}, nil
	}
	return &clusterpb.RemoteListenResponse{Ok: false, Message: fmt.Sprintf("未实现 mapping_id=%s", req.GetMappingId())}, nil
}
