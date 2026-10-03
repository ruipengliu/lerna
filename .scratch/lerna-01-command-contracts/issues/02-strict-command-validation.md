# 02: 严格命令解析与验证

**What to build:** 调用方可以把完整命令信封和 command.get 请求交给公开合同验证入口；Go／TypeScript 对合法输入一致接受，对含混、未知或越界输入返回明确的公共错误。

**Blocked by:** 01 — Go／TypeScript 准确编码往返。

**Status:** resolved

- [x] 共同命令信封明确 contract_version、profile、command_id、target、method、payload、accept_before、可选 trace_context 和方法要求的 expected_revision；创建、更新、查询的修订要求按方法定义，不为查询强行要求并发修订。
- [x] command.get 输入使用原命令的准确身份及 owner，具备闭合 Schema、前提和公共错误约定；不依靠名称、目标文本或“最新版本”猜测引用。
- [x] 从原始 JSON 字节进入的公开入口，在信息丢失前拒绝重复键，包括嵌套对象中的重复键；不能先解码成覆盖重复键的对象再声称完成严格验证。
- [x] 未知字段、非法枚举、非有限数值、缺失必填字段、错误字段类型、越界值与超大正文明确拒绝；冻结大小和范围限制，并为每类拒绝提供对应合法边界对照。
- [x] 错误版本不能静默降级或按默认版本解析；不同方法的 payload 不得互相替用，未经登记的方法或 profile 不得进入业务处理。
- [x] 公共错误采用闭合可判别类型，覆盖 schema_invalid、version_unsupported、forbidden、expired、idempotency_conflict、revision_changed、budget_exhausted、dependency_unavailable 和 unsupported；不靠错误字符串匹配判断业务分支。
- [x] Schema、生成类型和正反例同版更新；Application／Component 的 Go／TypeScript 公共验证入口对共同夹具给出一致结果，测试不依赖私有实现布局。

## Scope

本任务验证合同，不实现数据库接纳、持久去重、网络传输或 Task 状态迁移。用于说明通用信封的写方法夹具不得作为方法已开放的证据。

## Comments

Implemented 2026-10-03 in the isolated ticket branch, based on ticket 01's verified exit. Machine contract and Go / TypeScript SDK remain exact version `1.0.0`; generator version `1.0.0`. Contract version has not yet been published outside this slice. Dynamic-envelope detail follows `envelope-decisions.md`; no future method was opened for test purposes.

Public seams: Go `ParseCommand` / `DecodeCommand` / typed `Decode` / `Encode` / `Validate`, and corresponding TypeScript entries. Common-envelope success proves syntax and format only. The registered method boundary selects the complete `command.get` Schema, rejects read revision preconditions, and requires the target to match the original CommandRef exactly. Public errors use a closed code, with local causes explicitly excluded from the wire projection.

Observed red → green cycles: missing closed PublicError and classified command refusal; typed future-method envelope missing; TS accepting an undeclared envelope field; missing command.get boundary; wrong target kind/ref initially accepted; original programmable payload accepting a JSON number, non-JSON function, or excessive depth; local Go Cause initially serialized and TS public projection missing; generation initially permitting a nested dynamic payload inside a registered method. Each failure was observed at its public seam before the corresponding implementation passed. Version/method refusal, duplicate-key handling and frozen bounds also have regression evidence using the mature strict parser and validators inherited from ticket 01.

Verification: `make bootstrap`, `make check` and `make test-race` passed with the repository-pinned Go 1.27.1, Node 24.19.0, pnpm 12.8.1 and TypeScript 7.0.2. `make check` includes formatting, Go vet, strict TS compilation, generation consistency, 6 fail-closed generation probes, both language behavior suites, actual cross-language typed roundtrips and builds. Shared corpus: 41 value fixtures plus 68 command fixtures (20 accepted and 48 rejected command cases); all positive cases completed real Go→TS and TS→Go encode/decode. Rejected command fixtures matched the exact public error codes in both implementations. Frozen byte/depth bounds have exact-limit controls; payload properties are accepted at 1024 and refused at 1025. Re-generating twice produces no generated difference. `git diff --check` passed.

Limits: syntax verification does not authenticate a subject, query a fact source, judge expiry against a clock, admit work, preserve a durable receipt or guarantee network/storage behavior. Those responsibilities remain in tickets 04/05 and later slices. This resolves ticket 02 only; the slice remains in progress.
