# 实现进度

集成分支：`codex/lerna-implementation`。实现前基线：`1c042ec`。

2026-10-03 用户授权按依赖实现全部切片，逐片架构审查、提交并推送；需要决策时由 `gpt-6-astra`、`high` 智能体代为分析决定。后续 tickets 的粒度和依赖决策沿用此授权，不重复要求用户确认。测试沿用各规格已确定的 Application／Component／Host 公开验证范围。

## 当前状态

- 切片 01：**completed**。原任务 01–06 与后置架构任务 07 均 resolved；正确性修复、独立架构优化和 CI portability 已合入。受测实现提交 `23bac17ba0909c7a4d49d846eb08bc63391b99f0` 的真实远端 CI success，全部退出证据见[spec](../lerna-01-command-contracts/spec.md#切片退出证据2026-10-03)。切片02开始，03–22仍未开始。
- 任务 01 证据：锁定依赖干净重装、`make check`、`make test-race` 通过；41 项共同正反夹具及合法值的真实 Go→TS／TS→Go 往返通过。详细记录见[任务 01](../lerna-01-command-contracts/issues/01-exact-contract-roundtrip.md#comments)。这些只证明公共值合同范围。
- 任务 02 证据：`make bootstrap`、`make check`、`make test-race` 通过；共同语料共 109 项（41 值、68 命令），两端错误分类及合法输入真实往返通过；6 项生成器拒绝探针通过。详细记录见[任务 02](../lerna-01-command-contracts/issues/02-strict-command-validation.md#comments)。已按决策消除 TypeScript 内部模块反向导入公共 facade 的循环依赖。
- 任务 03 证据：`make check` 通过；46 项共同摘要案例（31 独立预期、15 拒绝），覆盖 UTF-16 键排序、业务／主体变化、重传及 1 MiB 边界。详细记录见[任务 03](../lerna-01-command-contracts/issues/03-canonical-command-digest.md#comments)。
- 任务 04 证据：合并后 `make check`、`make test-race`，最终别名修复后 `make lint test build` 与生成一致性通过。共同语料为 153 项值／命令／响应往返夹具，加 46 项摘要案例；最终 21 个 TS 测试。详细记录见[任务 04](../lerna-01-command-contracts/issues/04-receipt-and-readonly-query.md#comments)。只读接口返回独立观察，并保留独立的原输入引用，避免调用方或回调修改绑定及源事实；受信入口仍待任务 05。
- 任务 05 证据：`make check`、`make test-race` 通过；补充拒绝案例后 Go / TS 受信查询套件与 lint 通过。28 项共同权限夹具覆盖租户、委托、固定 owner、目录接替、原身份与截止；额外验证只读观察、回调隔离及取消。详细记录见[任务 05](../lerna-01-command-contracts/issues/05-authenticated-owner-isolation.md#comments)。Go 协作者须遵守有限 context；TS 单一总计时器限制整个查询，不能强制终止不合作的外部回调。这些不构成真实网络认证或持久事实查询证据。
- 任务 06 证据：`make check`、Go component race、合入前最新分支的生成一致性与 diff 检查通过；共同夹具包括 158 项编解码双向往返、46 项命令摘要、28 项受信查询、15 项协商与 2 项独立 Schema 黄金摘要；13 项生成器拒绝、8 项变更敏感性探针通过，32 个 TS 测试。详见[任务 06](../lerna-01-command-contracts/issues/06-version-and-method-negotiation.md#comments)。
- 两轴审查固定 `b825c2d`，发现 Go 默认转义膨胀导致合法往返拒绝，以及 TS 可变 Schema 放宽规则但摘要不变；另有 Unicode predicate 重复维护建议。单一修复已通过 `make check`、`make test-race`（34 个 TS 测试），既有摘要不变；修复与后续证据见[审查记录](../lerna-01-command-contracts/code-review.md)。验收尚未完成。
- gh 的 Actions API 查询返回 Forbidden；后续已连接 GitHub 工具实际核实 `4874226` / `70cbf66` 两次 push CI 均因格式脚本 `rg ENOENT` 失败。正在独立修复并将核对新提交实际 CI，不能把 Git push 成功当 CI 成功。详见[CI 记录](../lerna-01-command-contracts/ci-verification.md)。
- 切片完成必须有实际验收证据；规格明确、任务发布和代码合并均不等于验收完成。
- 真实外部接入、独立组件、生产故障域、评测样本与容量结论按验收矩阵分别记录，不用本地模拟结果代替。

## 过程约定

- 每个实现任务使用独立 worktree／分支，先核对集成分支基线，采用 TDD 后合并集成分支最新提交再报告。
- 任务依赖以实际完成记录为准；集成后运行相关验证及两轴代码审查，修复后关闭任务。
- 每片结束执行架构审查，按用户授权由决策智能体选择需要立即解决的问题，保留审查报告与决定，不增加尚无使用者的抽象。
- 记录命令、环境、准确版本、结果和限制后更新切片及验收索引，提交并推送。

## 切片 01 后置优化检查点

任务 07 已 resolved，基线同负载实测 119.217268 秒，最终 10.909816 秒；Go / TS fixture runner 各由 226 次启动降为 1 次。额外反序与 16 项生命周期测试成本单独记录，详见[架构审查](../lerna-01-command-contracts/architecture-review.md)；本机测量不作为生产容量保证。后续独立 Standards / Spec 审查均为 0 新发现，原问题已关闭；整片仍等待真实新 CI 成功与退出证据。

## 切片 01 完整退出

准确代码 `23bac17`，合同 1.0.0 仅 command.get，生成器 1.0.0。本地 check / 受影响 race、两轴复查、独立架构优化和 [远端 CI](https://github.com/ruipengliu/lerna/actions/runs/37141974245) 全部通过；旧 CI 失败保留并明确修复。6 项 spec 验收逐项映射、环境及限制见 spec Further Notes。尚无持久接纳、网络认证或业务组件第二实现；F02 摘要证据不替代切片02的数据库去重，G3 互操作仍待15。此前等待 / in-progress 记录为过程检查点，不代表当前状态。下一可开始切片是02。

## 切片 02 启动

前置01完整退出后，按已授权决策发布[八张票据](../lerna-02-durable-work/ticket-review.md)，共53项验收。内部依赖01→02/03，02+03→04，04→05/07/08，05→06。首票PG原子接纳已 claimed；采用独立 Host 版本 host-durable-work-1，不扩展公共1.0.0清单；真实数据库、迁移、耐久及进程故障均待产品验证。前片九个已完成 implementer worktree 已 clean 安全清理。

切片01退出文档 `8dc82ca` 和02票据检查点 `8e7438e` 的后续 push CI也实际 success（runs 37142242216 / 37142480320）；原受测实现23bac17不变。02首票目录裁决见 layout-decision.md，业务演示不进入host装配职责。

## 切片 02 首票合入与新 frontier

PG票01已 resolved并合入 `f12fab1533df5869d45d1504b4630381bd4955fc`，真实18项PG行为、本地完整检查及受影响race通过；COMMIT确认丢失与Host答复丢失分开验证。v1真实writer `988f8b7`、fixture `e808aae`及独立恢复证据已保留，准确来源见票01。新远端PG job待push后核验，不能把本地成功说成CI成功。

票02 SQLite接纳、票03 PG领取修订均已 claimed，从同一最新integration建立独立worktree并行推进。两票不共用写工作树；03/02冲突通过merger处理，接口按实际消费最小扩展。Claim、SQLite、调度、进程SIGKILL及切片02整体尚未验收。03后续六票/42AC准备经授权代理审定仍在/tmp，必须等02整片退出后按实际runtime复核，未发布。

PG 首票检查点 `e5f26b87fb8914dc16bb6837abff6607a50cddb1` 的真实远端 [CI 37145113569](https://github.com/ruipengliu/lerna/actions/runs/37145113569) 已 success：基础合同 job 与真实 PG 集成／race job 均通过，原 v1 artifacts 校验和通过；集成 1.778s、race 3.551s，日志未使用测试结果缓存。准确证据及范围见[切片02 CI记录](../lerna-02-durable-work/ci-verification.md)。此前“待push后核验”为历史检查点。

05/06 的[调度细化](../lerna-02-durable-work/scheduling-decisions.md) 已由授权决策代理分析并采用：同一纯 project 的可信持久策略、等待门槛、有限尝试、责任关闭与成功投影分别观察；同库数据库配额及有限竞争公平界。这只是待实施决定，05/06仍未开始，依赖图不变。并行新迁移在整合时核对编号，不改已发布来源。

票03 PG领取已 resolved，feature `18b80ce`、worker tip `f83f1b3` 经merger合入 `e8e3384`。真实29项PG测试、本地check/race及integration-race通过；准确 push [CI 37146622433](https://github.com/ruipengliu/lerna/actions/runs/37146622433) 也实际 success，集成2.534s、race4.866s且非cached。票02尚在实施，票04仍等待02完成；后续等待／配额／墓碑／进程恢复均未验收。

票02 SQLite接纳已 resolved，真实v1 writer `f4fb057`、fixture `80aebbf`、clean worker `874db65`经merger合入 `770f764`；共享13项接纳行为，两库顺序最终integration7.501s、race15.058s通过。03仍只有PG Claim，SQLite Claim待04；目前原八张核心票已完成01/02/03。

04已 claimed，在同版实际双库基础实现SQLite Claim和共同work suite。并行真实PG suites暴露不同schema共享advisory key的确定性锁耦合，失败保留在02Comments；授权代理决定修正存储锁范围，额外独立[票09](../lerna-02-durable-work/issues/09-pg-storage-lock-scope.md)已 claimed，与04并行，不修改原业务依赖图。当前只是决定，修复仍待真实red→green及并行复验。准确范围见[锁范围决定](../lerna-02-durable-work/pg-lock-scope-decision.md)。

`930d3ca` 真实双库接纳/PG领取 push [CI 37147496071](https://github.com/ruipengliu/lerna/actions/runs/37147496071) 已 success：基础合同、PG+SQLite必需集成2.868s/race8.739s均通过且-count=1重新执行，两份v1校验和全部OK。不包含尚未合入04/09的行为。

额外09已 resolved，feature `eed3ef8`经merger合入 `edb189e`：跨schema真实Claim空/Record锁超时先red，统一三路径schema tuple后四项正常与竞争green，完整两套integration/race并行9.583s/17.244s通过。09未修改迁移/源fixture/公开生成物，有限hash碰撞和同schema升级须排空旧协调协议的限制已记录；准确新远端CI待核验。04仍active，原八票01/02/03完成，不提前关闭整片。

## 切片 02 SQLite 接纳票据检查点

票02已 resolved；实际 v1 writer `f4fb057` 在代码提交后从历史 Git archive 构建，完整 SQLite file 与 checksum/provenance 保留。PG与SQLite共同运行13项同版接纳行为；SQLite WAL/FULL运行时核验、Linux跨进程写Host排除/关闭接替、busy、取消、完整Tx关闭生命周期、scope和真实v1文件恢复通过。实际内置SQLite3.53.4/go-sqlite3v1.14.52/CGO，不依赖个人头文件。有限Close失败时保留锁，回调退出后可重试。具体命令与范围见[票02 Comments](../lerna-02-durable-work/issues/02-sqlite-durable-admission.md#comments)。

切片02仍 in-progress；票03 PG Claim已合入，SQLite Claim留给04，调度/SIGKILL/完整升级出口尚未完成。两库必需integration与CI race已加-count=1；缺PG配置/服务或CGO均硬失败。新SQLite远端tip CI待root核实，历史PG成功不替代新两库CI。默认/tmp为tmpfs，实际最终两库integration/race和v1writer另在/workspace下本轮自登记overlayfs临时范围运行；未宣称断电或生产故障域耐久。
