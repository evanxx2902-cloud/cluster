package tunnel

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"

	clusterpb "cluster/proto"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

type Service struct {
	clusterpb.UnimplementedTunnelServiceServer
}

func NewService() *Service {
	return &Service{}
}

func (s *Service) OpenTunnel(stream clusterpb.TunnelService_OpenTunnelServer) error {
	ctx := stream.Context()

	first, err := stream.Recv()
	if err != nil {
		return err
	}
	open := first.GetOpen()
	if open == nil {
		return fmt.Errorf("首帧必须是 open")
	}
	if open.GetTunnelId() == "" {
		return fmt.Errorf("open.tunnel_id 为空")
	}

	conn, err := Dial(ctx, open.GetTarget())
	if err != nil {
		return err
	}
	defer conn.Close()

	g, ctx := errgroup.WithContext(ctx)

	// stream -> conn
	g.Go(func() error {
		for {
			fr, err := stream.Recv()
			if err != nil {
				if err == io.EOF {
					return nil
				}
				return err
			}
			if fr.GetTunnelId() != open.GetTunnelId() {
				continue
			}
			switch k := fr.GetKind().(type) {
			case *clusterpb.TunnelFrame_Data:
				if len(k.Data) == 0 {
					continue
				}
				if _, err := conn.Write(k.Data); err != nil {
					return err
				}
			case *clusterpb.TunnelFrame_Close:
				return nil
			case *clusterpb.TunnelFrame_Open:
				// 忽略额外 open
			default:
				// ignore
			}
		}
	})

	// conn -> stream
	g.Go(func() error {
		buf := make([]byte, 32*1024)
		var seq uint64
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				seq = atomic.AddUint64(&seq, 1)
				if err := stream.Send(&clusterpb.TunnelFrame{
					TunnelId: open.GetTunnelId(),
					Seq:      seq,
					Kind:     &clusterpb.TunnelFrame_Data{Data: append([]byte(nil), buf[:n]...)},
				}); err != nil {
					return err
				}
			}
			if err != nil {
				if err == io.EOF {
					_ = stream.Send(&clusterpb.TunnelFrame{TunnelId: open.GetTunnelId(), Kind: &clusterpb.TunnelFrame_Close{Close: true}})
					return nil
				}
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
	})

	return g.Wait()
}

// --- client helpers ---

type DialGRPCFunc func(ctx context.Context, addr string) (*grpc.ClientConn, error)

func OpenToTarget(ctx context.Context, dial DialGRPCFunc, remoteAddr string, tunnelID string, dir clusterpb.TunnelDirection, target *clusterpb.TunnelEndpoint) (grpc.BidiStreamingClient[clusterpb.TunnelFrame, clusterpb.TunnelFrame], error) {
	cc, err := dial(ctx, remoteAddr)
	if err != nil {
		return nil, err
	}
	cli := clusterpb.NewTunnelServiceClient(cc)
	st, err := cli.OpenTunnel(ctx)
	if err != nil {
		return nil, err
	}
	if err := st.Send(&clusterpb.TunnelFrame{
		TunnelId: tunnelID,
		Seq:      1,
		Kind: &clusterpb.TunnelFrame_Open{Open: &clusterpb.TunnelOpen{
			TunnelId:  tunnelID,
			Direction: dir,
			Target:    target,
		}},
	}); err != nil {
		return nil, err
	}
	return st, nil
}
