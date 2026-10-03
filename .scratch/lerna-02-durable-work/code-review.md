# 切片 02 两轴代码审查

基线：`8e7438e071727e25aa69e17fb81b2e53c416b78e`。准确审查 head：`26c91003e7cf09dcc42e8d8c90d0db7b65a915a5`。命令：`git diff 8e7438e071727e25aa69e17fb81b2e53c416b78e...26c91003e7cf09dcc42e8d8c90d0db7b65a915a5`；完整 commit list 已提供给两个独立只读审查者。

以下分别保留两轴原结论，不合并或重新排序。全部发现已交单一独立修复分支；当前尚未完成修复复核，绿色 CI 不关闭这些发现。

## Standards

完整变更范围包括 product/runtime、双库 storage/migrations、admission、scheduling/pools、lifecycle、conformance、scripts/CI 和 docs。只读审查；未重跑测试、操作数据库或修改仓库。依据 AGENTS.md、CONTEXT.md、相关 ADR/架构和实际 README；已执行的 tooling 检查不重复报告。历史未知 schema/CREATE 资源、SQLite storage-port 确认丢失和本机故障域限制全部保留。

1. **[P2，硬规则] 保留 PostgreSQL 启动错误的可判断原因。** `/workspace/lerna/adapters/postgres/store.go:44`、`:54`：`return nil, errors.New("PostgreSQL connection configuration rejected")` / `errors.New("PostgreSQL connection unavailable")` 丢弃 sql.Open/PingContext 原因。违反 `/workspace/lerna/AGENTS.md:105`：“错误必须保留可判断的原因”。已取消或超时的 startup context 因此不能通过 errors.Is 判断；认证/连接故障也不能通过 errors.As 分类。最小修复：提供脱敏 Error()、保留 Unwrap()/Is/As 的错误包装；不要直接输出 DSN 或未经处理的 driver 文本。

2. **[P3，硬规则] 同步当前容量实现的 README。** `/workspace/lerna/runtime/README.md:25`（“Fairness and quotas remain ticket06”）、`/workspace/lerna/adapters/sqlite/README.md:7`（“quotas remain ticket06”）、`/workspace/lerna/host/durablework/README.md:87`–88 仍宣称未实现，和同一 Host README:111 的“Current admission and processing require a durable finite pool”、CONTEXT 当前状态矛盾。违反 `/workspace/lerna/AGENTS.md:139` 文档须与实际实现一致。读者可能遗漏必需 pool 装配。删除陈旧句，链接当前 pool 配置/保证边界；整片待审查/CI 的事实继续保留。

3. **[P3，判断性 smell：Duplicated Code] 收拢同事务的 Claim 资格准备。** `/workspace/lerna/internal/durableworkdemo/step.go:121`–142 和 `:228`–249 重复 `w.Clock.Now` → `poolRepo.ValidatePoolClaim` → `schedule.ValidateClaim` → 锁后再次取时及双复核，前后的 pool/input 锁定也同形。未来修改一处门禁容易遗漏另一个实际入口。最小修复：在同文件提取事务内锁定/复核 helper，两处调用；保留锁后第二次取时，不把两次取时误当冗余。该项不是当前正确性缺陷，也不要求通用框架。

未把 Host aliases、后端特有 SQL、已采用同库 pool 协调或低层存储测试误判为职责/私表违例。

## Spec

Review pin: `8e7438e...26c9100`（完整 three-dot 变更；01–08 共53 AC及09的5 AC）。只读审查产品路径、现有真实测试设计及 adopted decisions；未启动数据库或重复测试。

## Finding

**[P2] Pool Run 固定 fallback 会错过合法重试窗口** — `/workspace/lerna/internal/durableworkdemo/pool_worker.go:148`。

采用的规格 `/workspace/lerna/.scratch/lerna-02-durable-work/scheduling-decisions.md:41` 明确要求：**“取‘最早相关 due/lease/deadline’与有限 fallback 扫描周期的较早值。”** `PoolWorker.Run` 每个独立lane在 `StepLane` 后却直接等待整个fallback，没有读取持久下一边界。两adapter都受影响。例如显式有限pool内接纳默认ordinary策略，设置 `ExecutionLimit=500ms`、`TransientFailures=1`、`BaseBackoff=100ms`、`MaxAttempts=3`，可信Clock正常推进，lease一分钟，Run fallback一秒。第一次真实Start后保存retry/due约T+100ms；runner到T+1s才再次服务，Maintain先保存expired，无第二次Start或成功Projection。将fallback改为10ms或使用无临时失败策略可正常完成；原固定applied回执保持。现有Pool Run正常对照 `/workspace/lerna/conformance/recovery/pool_test.go:895` 只采用10ms和即时成功，未覆盖此合法窗口。

最小修复：每lane有界查询全部声明members的持久due/lease及current、claimed两修订的deadline，以可信now计算较早未来等待；不能只复用anchor单owner的NextWake或跨owner写业务Tx；保留到期/跳锁项有限fallback，保持独立lane和无quota维护。增加两库真实Run retry窗口、非anchor成员/较新修订deadline及正常成功对照，禁止用延长期限掩盖。

## Remaining review

(a) 上述下一唤醒要求部分缺失；(b) 未发现未批准scope creep；(c) 上述实现会造成错误的可观察处理结果。其余58 AC未确认新缺陷：核对原键先于pool/截止、same-Store owner-bound Tx、真实PG Commit unknown、revision/epoch/锁后时间、严格Start/Finish与新work保留、有限FIFO动态尾部/独立lane、缺pool failclosed、无quota维护、真实v1/v2完整来源升级与body-gone、进程提交及旧消息正常/故障对照。SQLite确认丢失仅证明真实提交后的存储端口；原生Commit异常、断电及历史未知schema/CID清理仍是已接受证据限制。whole02仍需修复后复核、architecture review及准确最终CI。

Standards：3 项，轴内最严重 P2（错误原因丢失）；Spec：1 项，轴内最严重 P2（错过合法重试窗口）。

## 修复状态

`codex/durable-work-review-fixes` 正在处理全部四项，包括判断性重复门禁建议。要求保留锁后可信时间复核，覆盖全部有限 pool members 及 current/claimed revision 的实际下一边界；两库真实 Run 的短期限重试反例必须 red→green。修复合入后独立两轴复核、架构审查和准确新 CI 仍待完成。
