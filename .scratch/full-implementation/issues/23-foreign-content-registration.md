# 23 foreign-content-registration

Status: claimed
Blocked by: 01, 02
Implementer: memory_impl

依据：Memory 的跨 owner reference_intent／register_copy／held_copy_gate 合同，以及独立 Executor、外部 Agent、第二系统实现的准确来源交接。

实现明确且可关闭的 foreign Content 端口及宿主路由；源 owner、ContentRef、hash、版本与用途均不改写。网络登记、原字节及有限当前许可证明在 Tx 外取得；本方 Tx 内只核原 holder、准确范围、期限、已知撤回与来源门禁。nil 端口仍拒绝外部来源；普通镜像失联不开放新读取。派生来源与引用登记、清理分别恢复，原登记丢答复不换 copy_id。不得仅放宽 owner 比较，也不得使外部原件成为本方 Content 权威。共享参考进程的静态路由与签名密钥可显式装配，不声明跨库瞬时关闭。

## 完成依据

真实两个 owner／独立数据库证明原登记恢复、当前用途允许、跨租户／主体拒绝、原源关闭及失联拒新使用；证明独立设备输出能沿准确外部 ContentRef 被云端 Task 授权读取。其余跨 owner 材料使用均沿此端口，不建立隐藏捷径。

## Comments

2026-10-03：独立设备实现核对确认只读 bytes 路由不足以通过现有同 owner gate；从设计已有跨 owner 合同补入必要前置切片。
