# 切片 01 两轴代码审查

状态：发现项已修复，等待主任务合入与整体退出验收。两轴独立审查固定提交 `b825c2d`；修复与验证结果追加如下。

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

## 修复与回归证据

2026-10-03，单一实施者在 `codex/contract-review-fixes` 依据 `review-fix-decisions.md` 处理全部三个发现，不改变准确 1.0.0 机器合同或 Schema 摘要。

- Standards P2：保留 typed 原值预检，将 `json.Marshal` 的本地临时表示交给同一私有 strict parser（有限 6 × 1 MiB 预算），验证 Schema / 跨字段规则后复用 canonical writer 输出紧凑 UTF-8，再检查最终正文 1 MiB。公开 ParseJSON / Decode 上限仍为 1 MiB。回归先 red：`<`、`>`、`&`、U+2028、U+2029 五种准确 1 MiB 正文均可 Decode 却不能 Encode；修复后全部正常往返，增加一字节仍在 Decode 与 Encode 拒绝。字面量反斜杠转义与真实 Unicode 分隔符保持不同。
- Spec P1：生成器在导出 Schema 前递归冻结所有嵌套对象与数组。独立 node:test 进程在首次 command.get compile 前尝试把公开 additionalProperties 改成 true，旧实现接受额外字段，回归 red；修复后额外字段被拒绝、合法对照成功，继续改嵌套 const、required 数组和输出 oneOf 数组也不能改变公开源 Schema 或固定摘要。由生成器产生冻结代码，不手改生成物。
- Standards possible duplicated code：codec 原值检查复用已有 json.assertUnicode，并保持 boolean 返回；没有新增 barrel / 公共接口，也没有为这个内部去重增加镜像测试。

新增跨语言公开回归经 Go typed valuerunner 与 TS encode / decode 驱动准确 1 MiB 混合 HTML 字符、Unicode 分隔符与字面量转义，验证 Go→TS 和 TS→Go 的文本与大小，以及真实超限拒绝。既有 queryResult helper 曾比较 JSON 对象键顺序；紧凑 canonical 输出导致五个测试失败，已改为比较公开 ParseJSON 后的值，不把未承诺的键顺序当合同。

整片 01 保持 in-progress；这里的修复完成不替代主任务的架构优化和最终验收。

验证全部通过：锁定 `make bootstrap`、`make check`、`make test-race` 和 `git diff --check`。环境为 Go 1.27.1、Node 24.19.0、pnpm 12.8.1／TypeScript 7.0.2。check 包含格式 / vet / strict typecheck、生成零差异、13 个 generator 拒绝及 8 个 Schema 变更 goldens、全部 Go suite、34 个 TS tests、158 个共同编解码夹具双向往返、46 个独立命令 digest 案例、28 个受信读取场景、15 个协商案例和两个独立 Schema digest goldens，以及两语言构建。修复没有改变既有 command / Schema golden。原 200223 字节 Go probe 现可 Decode 和 Encode，最终输出仍为 200223 字节。真实数据库／网络／生产认证仍不在本次证据范围。
