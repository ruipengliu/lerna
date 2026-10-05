# 架构复核（静态）

固定d0fcc612／已接受b9db5cc；仅审四源diff。依据AGENTS.md、CONTEXT.md、ADR-0004/0005/0010及docs/architecture的依赖、运行、独立取证规则；原范围为issues/current-ci-partition.md。

必要架构冲突或扩权：0。三Content与二Durable组留在scripts入口，动态发现仍决定全集；literal仅为机械oracle，不成为业务权威。未改变事务、Claim、许可、业务测试或包120s，也未新增通用调度框架。重复flags的P3保持KEEP。

名义1565s与更严格单次1445s分开；不续期，不保证各段耗满或均衡耗时。机械通过不证明共享入口、新CI、生产容量或旧UNKNOWN关闭，仍需取证。本次无native、数据库、审计、格式化、产品修改、合并或推送。
