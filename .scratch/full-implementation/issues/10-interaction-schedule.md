# 10 interaction-schedule

Status: resolved
Blocked by: 01, 02, 03, 05

从 08 拆出可并行的 Session、Submission、Surface 与 Schedule 合同，完整依据 [实施规格](../spec.md)、[应用交互设计](../../../docs/architecture/interaction/README.md) 与根 AGENTS.md。保持原输入、原命令、准确 owner、截止与独立投递状态；明确 finite Calendar/DST 及持久单槽语义。只能开放通过正反例与真实存储恢复的接口。

## Comments

本方 Session/Submission、输入转交、Surface/Presentation 与有限 Schedule/Occurrence 已实现并由真实 SQLite/PG 验证。端口及开放边界见 [实现说明](../../../internal/interaction/README.md)。缺少能力端口的方法不登记；多请求 Surface 需真正负责方的批量请求门禁，缺少则 unsupported。原 owner/Command/Schema/规则版本/首次期限持久冻结；未知提交或回复丢失沿原身份恢复，未知单槽不释放。

本轮 `go test -race ./internal/interaction -count=1` 全域通过：SQLite 362.360s；HARNESS_INTERACTION_STORE=postgres 和实际 HARNESS_TEST_POSTGRES_DSN/私有命令环境下 PG 395.736s。两条真实 PG 反序锁曾各自 RED SQLSTATE40P01，批 Task→Content→Surface→Presentation 锁序及非权威 Peek 后强核均 GREEN。新增 101 分支完整分页/重开原游标、两条同 GoalRevision 的 steer 保序/原冲突不改值、规则更新后原 Occurrence 不刷新且新 overlap 不改槽；基本会话采用固定受信 Now，业务 TTL 未改变。vet、gofmt/diff、导航与架构设计检查通过；两个独立 reviewer 的 Standards/Spec 轴进行中，闭票待审查完成。

跨 Orchestrator 的全局来源目录/coverage_complete 由集成工单另行装配，本包只证明本方关系。实际浏览器/TLSWSS、独立部署宿主、外部平台账户、生产容量及跨 AZ 恢复分别由对应集成/平台证据判断，不由本方测试推断。

## Answer

本工单的本方 Session/Submission、准确输入转交、Surface/Presentation 与有限 Schedule/Occurrence 参考合同已完成。方法、实际端口与开放边界见 [实现说明](../../../internal/interaction/README.md)。缺少真实能力端口的方法保持不登记；全局来源目录和部署前提沿上面的边界由集成工单取证。

双轴审查完成：Standards 固定 `8435b46...f243a4d` 及增量 `c421739...0004bb6` 未确认硬规范违反或值得单列的 smell；Spec 原确认一项 P2——续页新游标延长首次截止，已由 `0004bb6` 修复并由独立 Spec reviewer 复核闭合，没有确认新的 Spec 缺陷。解析游标返回已核验原截止，续页与当前 QueryBinding 只取更早值，无绑定路径也保留首次期限。

三页公开 ListSessions 回归先 RED 4.251s：首游标 12:10 截止，12:09 续页后数据库重开，12:11 仍返回第三页；修复后拒绝为 cursor_expired。末版实际 `go test -race ./internal/interaction -run '^TestSessionCursor' -count=1`：SQLite PASS 69.203s；PG PASS 68.759s，覆盖真实 BindQuery 的精确输入摘要、较晚绑定不得延长和较早绑定收紧。受影响的 101 分支完整分页/重开追加 SQLite race PASS 24.999s，PG 分页与游标 race PASS 74.003s；此前全域两库 race 结果保留上文，不重复计作末版全域运行。

已无冲突合入最新 code-dev（含真实 Task 批请求门禁及原 dispatch 准备阶段），宿主/薄入口/本包编译、vet、gofmt/diff 与导航/架构检查通过。运行制品为 `/workspace/harness-dev-environment/interaction-verification.json`，分别固定全域实现 `f243a4d`、期限修复 `0004bb6`、Schema、环境、命令和故障点，未保存凭据。总体项目的 partial/blocked 状态以[实施覆盖](../../../docs/architecture/engineering/implementation-coverage.md)为准。
