# 机器契约唯一源

[共同调用规范](../docs/architecture/contracts/README.md) · [协议编码规则](../docs/architecture/contracts/protocol.md) · [方法索引](../docs/architecture/contracts/methods.md)

根目录统一维护 [Schema](schemas/protocol.schema.json)、[方法登记](schemas/methods.json)、[Protobuf 外壳](harness.proto)、[正反向向量](examples/README.md)及其准确字节。规范正文留在 `docs/architecture/contracts/`，不在文档目录再复制机器资产。

这些是未发布的 harness/1 草案资产。当前工程骨架只提供开发诊断，尚未实现领域方法、严格 Go/TS 线协议或 WSS/gRPC 互操作。资产路径迁移没有变更 JSON 字节、协议版本或业务语义；Proto 的 Go 生成包路径已对齐根 module。

```sh
make contracts-check
```

检查入口见 [tools](../tools/README.md)，生成文件不得成为第二份维护源。
