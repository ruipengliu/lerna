# 10 interaction-schedule

Status: claimed
Blocked by: 01, 02, 03, 05

从 08 拆出可并行的 Session、Submission、Surface 与 Schedule 合同，完整依据 [实施规格](../spec.md)、[应用交互设计](../../../docs/architecture/interaction/README.md) 与根 AGENTS.md。保持原输入、原命令、准确 owner、截止与独立投递状态；明确 finite Calendar/DST 及持久单槽语义。只能开放通过正反例与真实存储恢复的接口。

## Comments

本方 Session/Submission、输入转交、Surface/Presentation 与有限 Schedule/Occurrence 已实现并由真实 SQLite/PG 验证。端口及开放边界见 [实现说明](../../../internal/interaction/README.md)。缺少能力端口的方法不登记；多请求 Surface 需真正负责方的批量请求门禁，缺少则 unsupported。原 owner/Command/Schema/规则版本/首次期限持久冻结；未知提交或回复丢失沿原身份恢复，未知单槽不释放。

本轮 `go test -race ./internal/interaction -count=1` 全域通过：SQLite 362.360s；HARNESS_INTERACTION_STORE=postgres 和实际 HARNESS_TEST_POSTGRES_DSN/私有命令环境下 PG 395.736s。两条真实 PG 反序锁曾各自 RED SQLSTATE40P01，批 Task→Content→Surface→Presentation 锁序及非权威 Peek 后强核均 GREEN。新增 101 分支完整分页/重开原游标、两条同 GoalRevision 的 steer 保序/原冲突不改值、规则更新后原 Occurrence 不刷新且新 overlap 不改槽；基本会话采用固定受信 Now，业务 TTL 未改变。vet、gofmt/diff、导航与架构设计检查通过；两个独立 reviewer 的 Standards/Spec 轴进行中，闭票待审查完成。

跨 Orchestrator 的全局来源目录/coverage_complete 由集成工单另行装配，本包只证明本方关系。实际浏览器/TLSWSS、独立部署宿主、外部平台账户、生产容量及跨 AZ 恢复分别由对应集成/平台证据判断，不由本方测试推断。
