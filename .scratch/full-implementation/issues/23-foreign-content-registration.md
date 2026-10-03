# 23 foreign-content-registration

Status: partial
Blocked by: 01, 02
Implementer: memory_impl

依据：Memory 的跨 owner reference_intent／register_copy／held_copy_gate 合同，以及独立 Executor、外部 Agent、第二系统实现的准确来源交接。

实现明确且可关闭的 foreign Content 端口及宿主路由；源 owner、ContentRef、hash、版本与用途均不改写。网络登记、原字节及有限当前许可证明在 Tx 外取得；本方 Tx 内只核原 holder、准确范围、期限、已知撤回与来源门禁。nil 端口仍拒绝外部来源；普通镜像失联不开放新读取。派生来源与引用登记、清理分别恢复，原登记丢答复不换 copy_id。不得仅放宽 owner 比较，也不得使外部原件成为本方 Content 权威。共享参考进程的静态路由与签名密钥可显式装配，不声明跨库瞬时关闭。

## 完成依据

真实两个 owner／独立数据库证明原登记恢复、当前用途允许、跨租户／主体拒绝、原源关闭及失联拒新使用；证明独立设备输出能沿准确外部 ContentRef 被云端 Task 授权读取。其余跨 owner 材料使用均沿此端口，不建立隐藏捷径。

## Comments

2026-10-03：独立设备实现核对确认只读 bytes 路由不足以通过现有同 owner gate；从设计已有跨 owner 合同补入必要前置切片。

领域切片 `f0a9eff` / `683bb2f` 已实现原 reference_intent+Job、固定登记/释放命令、
准确字节+held gate、有限当前签名证明、control/use 分离、源关闭高水位、
原 owner 来源键、派生限制交集以及独立 Memory 元数据 holder 收尾。
纯 Tx `SourcePolicySnapshotTx` 用于独立 Executor 准入时冻结原 PolicyValues 和实际主体代次；
全部登记、当前查询、读取和释放在 Tx 外。nil 端口、旧凭据正文、失联和已知关闭均拒绝新读。

真实两 owner / 独立 SQLite 与独立 PG 数据库的公开接口矩阵通过：
SQLite foreign race 58.482s；PG foreign race 61.092s；完整 Memory normal 106.758s。
源原登记丢答复、原 intent 真实 COMMIT 后丢答复且零 RPC、原库重开、
正文 control 证明反例、跨租户/主体、较新凭据仅允许收尾、来源交集及 Memory 删除
仅清理自己的引用元数据均有实际正反例。具体命令、代码状态和边界存于
`/workspace/harness-dev-environment/foreign-content-verification.json`。

工单尚未关闭：独立设备输出经实际 SourceClient、云端 Task 授权的整条接线由宿主装配验证；
领域矩阵本身不证明该整链。普通跨 owner 使用为在线有限证明，未开放离线新使用或
跨 owner 独立派生保留证明。清理不把本地实际删除称为源端全介质 complete。
