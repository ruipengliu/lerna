# 06 authorization-governance

Status: partial
Blocked by:

依据 [实施规格](../spec.md) 与根 AGENTS.md，保留准确身份、负责方、事务、门禁和恢复。实际编译、公开接口行为、正反例及所需平台证据均通过后才关闭。

## Comments

2026-10-03：有界参考实现已集成，工单保持 partial；完整目标未验收。
领域与端口范围见 [治理实现说明](../../../internal/governance/README.md)，
实际宿主见 [受信参考宿主](../../../adapters/governance/README.md)。

已实现闭合公开合同及持久原身份：Grant issue/revoke/check/use/settle、父链交集、
once 永不复活、可信原业务确认一次消费、estimate acceptance 和 strict 离线租约合同；
完整 Evidence 依赖门禁、持续缺陷/holder/changes/ack/epoch 缺口和同库 Result notice；
BindingHead 当前 CAS、准备/激活/关闭/重开/处置与独立旧批准回退；
冻结 EvaluationPlan、sample-arm 唯一、formal/holdout 占用、曝光门禁、取消分页、
原分母封存和迟到资格失效。统计门槛与实际改善分别裁决，模型自述不构成权限或成功依据。

Linux 宿主仅执行明确 allowlist 的受信静态内置组件，核准确安装/制品/config/profile，
使用独占私有目录、持久原 instance/generation、真实文件自检和 cancel/wait/fence。
活动实例最多 128 个，只在实际退出后回收容量。
`reference-rule-file` runner 在两臂独立真实目录执行冻结的精确报告模板，
原 journal 先于写入，以臂外独立真值读回；丢回答查原 Attempt，未知不重写。
参考单样本改善通过而统计门槛失败时仍不授予正式资格。
[开发装配](../../../adapters/development/governance.go)使用真实 Memory 当前用途门禁，
只有目标 owner 的 worker 创建宿主；nil 配置保留能力关闭。

已实际验证真实 SQLite 和本机 PostgreSQL 的公开治理合同、原事务和同库接收故障回滚，
两库的真实参考 runner 冻结/执行/封存以及开发宿主当前 Content/独占所有权装配。
Linux 文件、原进程与 SIGKILL 测试覆盖实际退出、原实例不复活和未知写入不重放。
治理与 adapter race 检查通过；101 holder 通知验证第二页及同库接收故障回滚，
1001 样本取消保留 2002 个 sample-arm 分母，这只是取消分页证据。
可复现入口为 `go test -race ./internal/governance ./adapters/governance`；
治理 PG 套件需设置 `HARNESS_GOVERNANCE_POSTGRES_DSN`，开发装配 PG 用
`HARNESS_TEST_POSTGRES_DSN`。未配置或跳过的运行不记作通过。

仍未实现或缺外部前提：

- 任意不可信 native/WASI 扩展隔离、完整自定义 Skill/外部 Agent、跨宿主接管。
- 真实跨 owner Authority/连续传输、公司本人身份与密钥发放/轮换、独立设备离线租约部署。
- 真实 holdout 数据谱系与独立高影响校准，开放自然语言任务质量、通用模型改善和 live API 评测。
- 生产 autoUpdate/自动发布、完整遥测与供应商最终对账、三 AZ 耐久/容灾及目标容量验收。

缺正式数据或校准端口时拒绝正式资格；缺生命周期/runner/隔离前提时返回具体
`unsupported`、`blocked/not_run` 或保留原未知责任。参考模板、同库证明和本机平台测试
不能代表上述完整能力 resolved。总体边界见 [实施覆盖报告](../../../docs/architecture/engineering/implementation-coverage.md)。
