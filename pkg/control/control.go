package control

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"
	"time"

	clusterpb "cluster/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Dialer 统一管理到集群其他节点的 gRPC 连接（单端口复用）。
type Dialer struct {
	tlsCfg *tls.Config

	mu    sync.Mutex
	conns map[string]*grpc.ClientConn
}

func NewDialer(tlsCfg *tls.Config) *Dialer {
	return &Dialer{
		tlsCfg: tlsCfg,
		conns:  make(map[string]*grpc.ClientConn),
	}
}

func (d *Dialer) Get(ctx context.Context, addr string) (*grpc.ClientConn, error) {
	d.mu.Lock()
	if cc, ok := d.conns[addr]; ok {
		d.mu.Unlock()
		return cc, nil
	}
	d.mu.Unlock()

	creds := credentials.NewTLS(d.tlsCfg)
	cc, err := grpc.DialContext(
		ctx,
		addr,
		grpc.WithTransportCredentials(creds),
		grpc.WithBlock(),
		grpc.WithTimeout(5*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("连接节点失败 addr=%s: %w", addr, err)
	}

	d.mu.Lock()
	if existing, ok := d.conns[addr]; ok {
		d.mu.Unlock()
		_ = cc.Close()
		return existing, nil
	}
	d.conns[addr] = cc
	d.mu.Unlock()

	return cc, nil
}

func (d *Dialer) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	var firstErr error
	for addr, cc := range d.conns {
		if err := cc.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("关闭连接失败 addr=%s: %w", addr, err)
		}
		delete(d.conns, addr)
	}
	return firstErr
}

// ControlBroadcaster 负责在 Master 身份下向所有订阅者广播 ClusterEvent。
//
// 说明：这里的“订阅者”是通过 gRPC ControlService.SubscribeCluster 建立的 server-stream。
// Master 会把事件写入每个订阅者的 channel，由 handler 线程发送。
type ControlBroadcaster struct {
	mu sync.Mutex

	subs map[string]chan *clusterpb.ClusterEvent
}

func NewControlBroadcaster() *ControlBroadcaster {
	return &ControlBroadcaster{subs: make(map[string]chan *clusterpb.ClusterEvent)}
}

func (b *ControlBroadcaster) AddSubscriber(nodeID string) chan *clusterpb.ClusterEvent {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan *clusterpb.ClusterEvent, 64)
	b.subs[nodeID] = ch
	return ch
}

func (b *ControlBroadcaster) RemoveSubscriber(nodeID string) {
	b.mu.Lock()
	ch, ok := b.subs[nodeID]
	if ok {
		delete(b.subs, nodeID)
	}
	b.mu.Unlock()

	if ok {
		close(ch)
	}
}

func (b *ControlBroadcaster) Broadcast(ev *clusterpb.ClusterEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, ch := range b.subs {
		select {
		case ch <- ev:
		default:
			// 丢弃慢订阅者的事件，避免阻塞广播。
		}
	}
}
