# cluster

一个 Go 语言实现的高可用集群组件（实验性）。节点间使用 **gRPC + mTLS** 在**单端口**上通信，提供：

- 选举（VRRP-like，无多数派）：Master / Backup / Slave
- 控制面广播：Master 向全体订阅者推送集群状态
- 隧道/端口映射：通过 gRPC 双向流转发字节流（开发中）

> 重要限制：该选举算法不采用多数派/仲裁，在网络分区场景下**可能出现双 Master**。

## 目录结构

- [proto/](proto/)：protobuf 定义与生成的 Go 代码
- [pkg/election/](pkg/election/)：选举心跳与本地 leader 计算
- [pkg/control/](pkg/control/)：控制面广播与 RPC
- [pkg/config/](pkg/config/)：JSON 配置加载 + mTLS 配置加载
- [config/example.json](config/example.json)：配置示例

## 开发要求

- Go module：`module cluster`
- 节点间 gRPC 必须使用 mTLS（双向证书校验）

## 生成 protobuf

仓库内 `proto/cluster.pb.go` / `proto/cluster_grpc.pb.go` 为生成文件。

```bash
# 需要已安装：protoc、protoc-gen-go、protoc-gen-go-grpc
PATH="$PATH:$(go env GOPATH)/bin" \
  protoc --go_out=. --go-grpc_out=. \
  --go_opt=paths=source_relative --go-grpc_opt=paths=source_relative \
  proto/cluster.proto
```

## 当前进度

- 已实现：ElectionService（双向心跳流）、ControlService（订阅广播、手工切换接口）
- 已加：ControlService.StartRemoteListen RPC（作为“反向映射”控制面的预留入口；当前仅桩实现）
- 待实现：TunnelService.OpenTunnel 的完整字节流转发、端口映射执行、HTTP API（/elect /nodes /subscribe）
