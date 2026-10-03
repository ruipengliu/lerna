# 01: Go／TypeScript 准确编码往返

**What to build:** 应用和组件开发者可以用同版 Go／TypeScript 公共类型构造准确引用、修订、金额、时间和集合视图，经公开编解码入口往返后保留原值；新环境可以重建类型并运行共同夹具。

**Blocked by:** None (can start immediately).

**Status:** resolved

- [x] 从同一版闭合机器契约生成 Go／TypeScript 类型，覆盖 ID、OwnerRef、ObjectRef、ContentRef、Revision、Amount、Time 和 CollectionView；准确内容引用保留版本、摘要、介质类型和字节长度，不暗含读取权限。
- [x] 两种语言通过共同正反例验证不透明 ID、非负修订、带明确单位的整数金额和固定精度 UTC 时间；十进制字符串的格式、范围及时间精度明确且一致。超过 JavaScript 安全整数范围的合法值仍能准确往返，金额不经过浮点累计。
- [x] CollectionView 保留有界 items、cursor、exhausted、partial、gaps、读取范围与水位，不将分页视图解释为全局快照。
- [x] Go 编码后由 TypeScript 解码并重编码，以及反向路径，均保留准确值；边界正例可用，非法值得到明确验证失败，而非截断、取整或默认补值。
- [x] 使用根目录单 Go module 和 pnpm 工作区；验证并锁定工具链、生成器及依赖版本，构建说明不依赖当前机器的个人绝对路径。
- [x] 建立可运行的 bootstrap、generate、lint、test、test-contract、build 和 check 入口，CI 复用同一入口；只包含当前已实现范围，缺失必需依赖或失败不得返回成功。
- [x] 生成物标注来源、纳入版本控制且不得手工修改；锁定安装及重复生成无差异，共同夹具可以在无外部凭据的环境运行。

## Scope

本任务交付公共值的机器契约、编解码和验证设施；后续任务逐步增加命令、回执和方法。未实现能力不得用占位实现广告为已支持。


## Comments

### 2026-10-03 · 实现与验证证据

在 `codex/contract-ticket-01` 专属 worktree 实现；准确合同为 **1.0.0**，依照同切片 [技术决策](../decisions.md)。交付 `contract/schema/1.0.0/values.json`、生成 Go / TS 类型、公开严格 JSON 边界及值编解码、共同夹具和真实双向运行器。CollectionView 首版 items 为闭合 ObjectRef[]；单 owner 提交水位与分页缺口语义见 contract README。

TDD 均经已确认的 Application / Component 编解码／验证 seam：

- Go 准确 Revision tracer 的首次 `go test ./conformance/component` 报合同包缺失；提供 schema、生成类型与 Decode/Encode 后通过。
- TS 准确 Revision tracer 的首次 `pnpm test` 报 index.ts 不存在；提供公开 SDK codec 后通过。
- 内容版本／长度 tracer 首次报 ContentRef 未生成；由同版 Schema 生成后通过。
- 原始 JSON seam 首次 Go 报 ParseJSON 未定义，TS 报 parseJSON 未导出；严格词法边界实现后正常 Unicode / prototype 名称对照与重复键、非法字节、孤立 surrogate、数字 token、尾随值反例通过。
- CollectionView 共同反例首次报 exhausted=true 且非空 cursor 被接纳；Schema 增加条件约束后 Go / TS 均拒绝，并保留正常分页与部分视图。
- Go 非法 UTF-8 编码 tracer 首次报“encoding silently replaced invalid UTF-8”；编码前校验原始字符串后明确拒绝。TS 编码同样执行原始边界与完整 schema 回验。

验证环境：Linux amd64，Go **1.27.1**、Node **24.19.0**、pnpm **12.8.1**、TypeScript **7.0.2**、Prettier **3.6.2**。生成器 **1.0.0** 固定于 scripts/generate.mjs；Go jsonschema **v6.0.2**、Ajv 2020 **8.17.1** 由 go.sum / pnpm-lock.yaml 锁定。

已取得实际退出证据：

- `make bootstrap` 成功；删除本 worktree 的 node_modules 后重新执行 frozen-lockfile 安装仍成功，未使用个人绝对路径或外部凭据。
- `make check` 完整通过：只读格式、Go vet、TS strict 类型检查、生成一致性、Go 与 TS 公开测试、真实跨语言合同往返、Go / TS 构建。TS 包含 **5** 组公开边界测试；共同夹具 **41** 项（含 ID / 单位 / 集合 / cursor / gap 上限及越界、int64 极值、真实 Gregorian 日期、内容准确版本），所有正例经过真实 Go→TS 与 TS→Go 解码并重编码且准确值相同，反例在两种语言明确失败。
- `make generate` 后连续两次 `node scripts/generate.mjs --check` 零差异；临时 Schema 注入未知语义关键词 minimum 后生成器非零退出并明确报 unsupported schema keyword。
- 最终编码校验增强后 `pnpm test`、`make lint build`、`make test-race` 通过；`git diff --check` 通过。
- 自评覆盖七项验收、领域边界、闭合 Schema 与词法拒绝；没有为后续 profile 添加占位实现或空集成目标。

限制：本票仅公共值与工具链，没有命令方法、回执、真实网络、持久去重或 Task 推进。GitHub CI 配置复用本地入口；本记录不把本地执行宣称为远端 CI 已运行。整个 spec 仍由后续 02..06 tickets 完成。

切片双轴审查回归：紧凑 Go 编码现保留临近 1 MiB 的 HTML / U+2028 / U+2029 与字面转义，公开正文限制没有放宽；新增准确边界及真正超限拒绝的 Go / TS 双向测试通过。完整 make check（34 TS tests / 158 共同往返夹具）与 make test-race 通过，详见 ../code-review.md。
