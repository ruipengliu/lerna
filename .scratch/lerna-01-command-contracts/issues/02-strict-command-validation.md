# 02: 严格命令解析与验证

**What to build:** 调用方可以把完整命令信封和 command.get 请求交给公开合同验证入口；Go／TypeScript 对合法输入一致接受，对含混、未知或越界输入返回明确的公共错误。

**Blocked by:** 01 — Go／TypeScript 准确编码往返。

**Status:** ready-for-agent

- [ ] 共同命令信封明确 contract_version、profile、command_id、target、method、payload、accept_before、可选 trace_context 和方法要求的 expected_revision；创建、更新、查询的修订要求按方法定义，不为查询强行要求并发修订。
- [ ] command.get 输入使用原命令的准确身份及 owner，具备闭合 Schema、前提和公共错误约定；不依靠名称、目标文本或“最新版本”猜测引用。
- [ ] 从原始 JSON 字节进入的公开入口，在信息丢失前拒绝重复键，包括嵌套对象中的重复键；不能先解码成覆盖重复键的对象再声称完成严格验证。
- [ ] 未知字段、非法枚举、非有限数值、缺失必填字段、错误字段类型、越界值与超大正文明确拒绝；冻结大小和范围限制，并为每类拒绝提供对应合法边界对照。
- [ ] 错误版本不能静默降级或按默认版本解析；不同方法的 payload 不得互相替用，未经登记的方法或 profile 不得进入业务处理。
- [ ] 公共错误采用闭合可判别类型，覆盖 schema_invalid、version_unsupported、forbidden、expired、idempotency_conflict、revision_changed、budget_exhausted、dependency_unavailable 和 unsupported；不靠错误字符串匹配判断业务分支。
- [ ] Schema、生成类型和正反例同版更新；Application／Component 的 Go／TypeScript 公共验证入口对共同夹具给出一致结果，测试不依赖私有实现布局。

## Scope

本任务验证合同，不实现数据库接纳、持久去重、网络传输或 Task 状态迁移。用于说明通用信封的写方法夹具不得作为方法已开放的证据。
