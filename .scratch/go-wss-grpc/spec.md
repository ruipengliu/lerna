# Go 与长连接技术栈修订

用户确认核心采用 Go，独立服务使用 gRPC；2026-09-26 在 grill-with-docs 决策轮次中接受浏览器、CLI 和设备统一 WSS。HTTPS 保留发现、认证与大文件字节，上传／镜像管理通过 WSS／gRPC；同进程模块使用 Go interface。

## 决策树与范围

- 已定：核心语言 Go；浏览器与第三方生态工具语言保持适配边界。
- 已定：端云统一 WSS，不额外维护原生设备 gRPC 端云绑定。
- 已定：服务跨进程 gRPC，同进程不拆共同事务。
- 实现选择：Protobuf 外壳携带现有严格 JSON，复用领域 Schema／JCS；WSS 帧、发现、示例与检查同步修改。
- 语义不变：固定 Home、原命令恢复、Delivery／Reply／ReplyAck、当前披露资格、独立效果与业务控制。

正文修订覆盖部署、总览、共同与传输契约、相关模块和验收；不修改 archive，不迁移无关 UML 工作，也不宣称已有 Go 服务或运行性能结果。Go/WSS/gRPC 为通用技术术语，不加入领域词汇；集成取舍记录 ADR-0002。

## 验证

分别进行语义故障推演、文档链接和图示检查、领域与传输静态向量、Protobuf 描述符编译。实际网络、数据库、鉴权和高并发实验仍待参考实现。

## 完成记录

已同步技术总览、技术栈、生产拓扑、契约、Schema、向量和模块引用。独立语义审查发现的外壳容量、委托身份、镜像控制路径和远程 Brain 边界均已修订；重连图补齐新 ready。最终检查及未执行边界见 docs/architecture/review.md 第 14 节。

临时 Protobuf 校验环境为 /tmp/lerna-grpc-contract-check（grpcio-tools 1.80.0、protobuf 6.33.6）；harness.pb 与 harness_pb2.py 由 contracts/harness.proto 生成，仅用于静态封装往返验证。生产依赖版本仍由安装锁和实际兼容验收决定。
