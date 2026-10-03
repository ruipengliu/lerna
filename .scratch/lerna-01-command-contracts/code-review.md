# 切片 01 两轴代码审查

状态：修复中。两轴独立审查固定提交 `b825c2d`；后续验证结果追加到本记录。

## Standards

Reviewed SHA: `b825c2d59b7b58fd360dd7200d8783a159732de2`。基线：`1c042ec`；范围：`git diff 1c042ec...HEAD`。

硬性合同问题 1 项：

- **[P2] contract/codec.go:104–109：Go 编码器会拒绝可以合法往返的正文。** hunk 为 `encoded, err := json.Marshal(value)` 后调用 `Decode[T](encoded)`。默认 HTML 转义把 `<` 扩成 `\u003c`；公开 probe 的 200223 字节 CommandEnvelope 经 Go Decode 成功，Go Encode 却报 `body exceeds 1048576 bytes`，同版 TS decode/encode 成功且仍为 200223 字节。因此通用信封无法可靠完成同版往返／原内容重传。依据：AGENTS.md「合同与代码生成」要求 Go / TS 共用合同验证；contract/README.md:22 承诺同一 Schema 和最多 1 MiB UTF-8 正文。建议编码选择不膨胀合法 Unicode 的 JSON 表示，再检查实际输出大小；保留超限拒绝。复现：`/tmp/lerna-standards-probe.go`。TS probe 使用 contract-06 已有依赖，其相关源码对 reviewed SHA 无差异。

判断性 smell 1 项：

- **Possible Duplicated Code：sdk/typescript/src/codec.ts:60–69 与 json.ts:5–14。** 两个 hunk 都执行 `code >= 0xd800 && code <= 0xdbff`、消费下一单元、拒绝孤立低 surrogate；区别仅为 false／throw。建议复用一个 Unicode scalar predicate，保留边界所需返回形式，避免严格解析和程序值验证规则漂移。属于低优先级维护判断，不是现存行为缺陷，也不需要增加接口或镜像测试。

未发现其他需报告的 documented-standard 违背或 baseline smell；生成文件、锁文件和格式规则未重复报告工具已覆盖的检查。

## Spec

Reviewed SHA: `b825c2d59b7b58fd360dd7200d8783a159732de2`。Baseline: `1c042ec983a35cb46c758b1fcabb7378ac08c5e8`。

- **[P1] TS 可变 Schema 能在摘要不变时放宽已协商合同。** Ticket 06 第 11 行要求“新增可选字段同样必须通过版本协商，不静默采用默认值或扩大解释范围”，第 13 行要求“支持清单、闭合 Schema、生成 Go／TypeScript 类型及正反例同版”；spec 验收 2 要求“未知字段……均拒绝”。`sdk/typescript/src/index.ts:2` 重导出机器 Schema；`src/generated/values.ts:302` 仅用 `as const`，运行时未冻结。`src/codec.ts:55` 将同一可变对象注册到 Ajv，`:94` 惰性编译。首次解析前执行 `schema.$defs.CommandGetPayload.additionalProperties = true`，`decodeCommand` 即接受 `payload.unexpected:"extra"`，而公布的输入 Schema 摘要保持 `sha256:bd37d7bb6f69006352bc74c27d04e13faaac912812d28f91c36794d6dbb8278b`。这使两种语言对同版合同产生不同判定，协商不再描述实际验证规则。应隔离验证器私有 Schema 与公开数据，或在注册前递归冻结机器 Schema，并补正常闭合对照与公开元数据变更探针。

未发现其他缺失、部分实现或未要求的范围扩张。真实 DB／网络／生产认证明确在本片范围外。

核验：Go component suite 通过；同源码 contract-06 worktree 的 32 个 TS 公开测试通过。独立 Python 重算 31 个命令黄金字面量及 2 个可达 Schema 黄金摘要均匹配。上述缺陷用 `/tmp/lerna-01-schema-alias-probe.mjs` 复现；root 未安装 node_modules，因此使用已安装 worktree，codec 与 generated 文件的 SHA-256 已确认等于固定 HEAD。仓库未修改。

## 汇总与处理

Standards：1 项行为问题、1 项判断性 smell；本轴最严重问题为 Go 编码膨胀。Spec：1 项行为问题；本轴最严重问题为可变 Schema 与协商摘要不一致。全部交由同一修复任务处理。
