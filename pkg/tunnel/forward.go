package tunnel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"sync/atomic"

	clusterpb "cluster/proto"

	"golang.org/x/sync/errgroup"
)

type ForwardRule struct {
	Listen     *clusterpb.TunnelEndpoint
	RemoteAddr string
	Target     *clusterpb.TunnelEndpoint
}

func RunForwardRule(ctx context.Context, dial DialGRPCFunc, rule ForwardRule) error {
	ln, err := Listen(rule.Listen)
	if err != nil {
		return err
	}
	defer ln.Close()

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		<-ctx.Done()
		_ = ln.Close()
		return nil
	})

	for {
		c, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				return err
			}
		}
		c := c
		g.Go(func() error {
			defer c.Close()

			tunnelID, err := newTunnelID()
			if err != nil {
				return err
			}
			st, err := OpenToTarget(ctx, dial, rule.RemoteAddr, tunnelID, clusterpb.TunnelDirection_TUNNEL_DIRECTION_FORWARD, rule.Target)
			if err != nil {
				return err
			}

			// conn -> stream
			sg, sctx := errgroup.WithContext(ctx)
			sg.Go(func() error {
				buf := make([]byte, 32*1024)
				var seq uint64 = 1
				for {
					n, err := c.Read(buf)
					if n > 0 {
						seq = atomic.AddUint64(&seq, 1)
						if err := st.Send(&clusterpb.TunnelFrame{TunnelId: tunnelID, Seq: seq, Kind: &clusterpb.TunnelFrame_Data{Data: append([]byte(nil), buf[:n]...)}}); err != nil {
							return err
						}
					}
					if err != nil {
						if err == io.EOF {
							_ = st.Send(&clusterpb.TunnelFrame{TunnelId: tunnelID, Kind: &clusterpb.TunnelFrame_Close{Close: true}})
							_ = st.CloseSend()
							return nil
						}
						return err
					}
					select {
					case <-sctx.Done():
						return sctx.Err()
					default:
					}
				}
			})

			// stream -> conn
			sg.Go(func() error {
				for {
					fr, err := st.Recv()
					if err != nil {
						if err == io.EOF {
							return nil
						}
						return err
					}
					if fr.GetTunnelId() != tunnelID {
						continue
					}
					switch k := fr.GetKind().(type) {
					case *clusterpb.TunnelFrame_Data:
						if len(k.Data) == 0 {
							continue
						}
						if _, err := c.Write(k.Data); err != nil {
							return err
						}
					case *clusterpb.TunnelFrame_Close:
						return nil
					case *clusterpb.TunnelFrame_Open:
						// ignore
					default:
						// ignore
					}
				}
			})

			return sg.Wait()
		})
	}
}

func newTunnelID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成 tunnel_id 失败: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// compile-time check that net.Conn methods used
var _ net.Conn
