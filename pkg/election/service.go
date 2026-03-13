package election

import (
	"io"
	"time"

	clusterpb "cluster/proto"

	"golang.org/x/sync/errgroup"
)

// Service 实现 ElectionServiceServer。
//
// 说明：这是一个双向 stream。
// - 对端发送的心跳，我们 Recv 并写入观测表
// - 我们也会定期 Send 自己的心跳给对端
//
// 这样无论流是谁发起（client dial），两端都会持续收发。
type Service struct {
	clusterpb.UnimplementedElectionServiceServer

	client *Client
}

func NewService(client *Client) *Service {
	return &Service{client: client}
}

func (s *Service) Heartbeat(stream clusterpb.ElectionService_HeartbeatServer) error {
	ctx := stream.Context()

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		for {
			hb, err := stream.Recv()
			if err != nil {
				if err == io.EOF {
					return nil
				}
				return err
			}
			s.client.ObserveFromHeartbeat(time.Now(), hb)
		}
	})

	g.Go(func() error {
		ticker := time.NewTicker(s.client.heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				self := s.client.Self()
				hb := &clusterpb.ElectionHeartbeat{
					ClusterId:       self.ClusterId,
					From:            self,
					TimestampUnixMs: time.Now().UnixMilli(),
				}
				if err := stream.Send(hb); err != nil {
					return err
				}
			}
		}
	})

	return g.Wait()
}
