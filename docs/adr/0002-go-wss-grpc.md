# Go 核心，端云 WSS，服务间 gRPC

为支持浏览器与设备的双向交互和服务端主动推送，采用 Go 实现核心、宿主与默认组件，端云统一使用 WSS；独立服务间采用 gRPC，同进程直接调用 Go 接口。相较设备单独使用 gRPC 双向流，统一 WSS 减少一套端云绑定；代价是维护应用帧、流控和重连机制，连接标识始终不能替代原命令或操作标识。

gRPC 使用 Protobuf 外壳携带现有严格 JSON，避免同时维护领域方法的两份字段权威，承担额外编码与运行时校验成本。发现、认证与大内容原始字节保留 HTTPS；上传／镜像元数据与控制沿 WSS／gRPC；内容、命令回执和未决责任继续由原负责方保存。完整约束见[技术基线与宿主装配](../architecture/deployment.md)、[端云协议](../architecture/contracts/transport.md)和[gRPC 绑定](../architecture/contracts/grpc.md)。
