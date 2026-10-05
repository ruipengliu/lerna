# Run cancellation：架构评估

修复落实切片02持久Run生命周期、切片03有限故障规则及根AGENTS的原因保留要求，采用1b097620决定。[资格证据](current-ci-cancellation-qualification.md)保存机械首RED及实际PG资格。现有ADR的owner、短事务和提交未知约束继续适用，无新领域决定替代ADR。

产品只改internal/durableworkdemo/pool_worker.go：保存原caller context；三个lane使用同一child context，buffer收集三条真实命名结果；首次结果触发内部停止，Wait join全部lane后errors.Join真实原因；原caller.Err非nil才加入caller原因。child取消不伪造caller取消，driver/CommitUnknown及合法sibling原因保留。

接口沿用消费方Runner/Timer与Host.NewPoolWorker/Run seam，Host只装配。两条有限机械测试证明caller取消与caller-live第一故障；真实PG正常release/取消对照证明原Core SQL阻塞、实际join和公开责任尾。原opaqueDSN失败独立保留，parseable角色标签与普通准备连接属于Host夹具，没有产品PoolScope、token或业务入口改变。

Runtime Core、Claim/fence、SQL/迁移、policy/quota/lease及合同/profile沿用。原PG/SQLite Run/wake/locked-page N/R、规范makecheck及完整非integration moduleR通过；独立Standards/Spec必要代码findings为0。资源观察保留原不完整身份和UNKNOWN，物理状态不替代logicalClose。

无03/06未交付产品的隐藏依赖。此说明只记录固定357/product65本地资格；primary merge/push与新CI待完成，repair issue继续claimed。
