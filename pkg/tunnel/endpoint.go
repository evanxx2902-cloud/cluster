package tunnel

import (
	"context"
	"fmt"
	"net"

	clusterpb "cluster/proto"
)

func networkString(ep *clusterpb.TunnelEndpoint) (string, error) {
	switch ep.GetNetwork() {
	case clusterpb.TunnelNetwork_TUNNEL_NETWORK_TCP:
		return "tcp", nil
	case clusterpb.TunnelNetwork_TUNNEL_NETWORK_UNIX:
		return "unix", nil
	default:
		return "", fmt.Errorf("不支持的 network=%v", ep.GetNetwork())
	}
}

func Dial(ctx context.Context, ep *clusterpb.TunnelEndpoint) (net.Conn, error) {
	if ep == nil {
		return nil, fmt.Errorf("endpoint 为空")
	}
	netw, err := networkString(ep)
	if err != nil {
		return nil, err
	}
	var d net.Dialer
	c, err := d.DialContext(ctx, netw, ep.GetAddress())
	if err != nil {
		return nil, fmt.Errorf("dial %s %s 失败: %w", netw, ep.GetAddress(), err)
	}
	return c, nil
}

func Listen(ep *clusterpb.TunnelEndpoint) (net.Listener, error) {
	if ep == nil {
		return nil, fmt.Errorf("endpoint 为空")
	}
	netw, err := networkString(ep)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen(netw, ep.GetAddress())
	if err != nil {
		return nil, fmt.Errorf("listen %s %s 失败: %w", netw, ep.GetAddress(), err)
	}
	return ln, nil
}
