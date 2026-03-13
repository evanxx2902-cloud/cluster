package config

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
)

// Config 集群配置
type Config struct {
	ClusterID      string          `json:"cluster_id"`
	SelfNodeID     string          `json:"self_node_id"`
	GRPCListenAddr string          `json:"grpc_listen_addr"`
	HTTPListenAddr string          `json:"http_listen_addr"`
	TLS            TLSConfig       `json:"tls"`
	Timing         TimingConfig    `json:"timing"`
	Nodes          []NodeConfig    `json:"nodes"`
	Mappings       []MappingConfig `json:"mappings"`
}

type TLSConfig struct {
	CAFile     string `json:"ca_file"`
	CertFile   string `json:"cert_file"`
	KeyFile    string `json:"key_file"`
	ServerName string `json:"server_name"`
}

type TimingConfig struct {
	HeartbeatIntervalMs int `json:"heartbeat_interval_ms"`
	ElectionTimeoutMs   int `json:"election_timeout_ms"`
}

type NodeConfig struct {
	NodeID   string `json:"node_id"`
	Addr     string `json:"addr"`
	Priority int32  `json:"priority"`
	RoleHint string `json:"role_hint"` // "MASTER" / "BACKUP" / "SLAVE"
}

type EndpointConfig struct {
	Network string `json:"network"` // "tcp" / "unix"
	Address string `json:"address"`
}

type MappingConfig struct {
	Direction    string         `json:"direction"` // "FORWARD" / "REVERSE"
	RemoteNodeID string         `json:"remote_node_id"`
	Local        EndpointConfig `json:"local"`
	Remote       EndpointConfig `json:"remote"`
}

// LoadConfig 从 JSON 文件加载配置
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	return &cfg, nil
}

// LoadTLSConfig 加载 mTLS 配置（用于 gRPC 客户端/服务端）
func LoadTLSConfig(tlsCfg TLSConfig) (*tls.Config, error) {
	// 加载 CA 证书
	caCert, err := os.ReadFile(tlsCfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("读取 CA 证书失败: %w", err)
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("解析 CA 证书失败")
	}

	// 加载节点证书和私钥
	cert, err := tls.LoadX509KeyPair(tlsCfg.CertFile, tlsCfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("加载节点证书/私钥失败: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    certPool,
		RootCAs:      certPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ServerName:   tlsCfg.ServerName,
		MinVersion:   tls.VersionTLS12,
	}, nil
}
