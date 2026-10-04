# 实现进度

集成分支：`codex/lerna-implementation`。实现前基线：`1c042ec`。

2026-10-03 用户授权按依赖实现全部切片，逐片架构审查、提交并推送；需要决策时由 `gpt-6-astra`、`high` 智能体代为分析决定。后续 tickets 的粒度和依赖决策沿用此授权，不重复要求用户确认。测试沿用各规格已确定的 Application／Component／Host 公开验证范围。

## 当前状态

- 切片 01：**completed**。原任务 01–06 与后置架构任务 07 均 resolved；正确性修复、独立架构优化和 CI portability 已合入。受测实现提交 `23bac17ba0909c7a4d49d846eb08bc63391b99f0` 的真实远端 CI success，全部退出证据见[spec](../lerna-01-command-contracts/spec.md#切片退出证据2026-10-03)。
- 切片 02：**completed**。10票68项AC resolved；最终源码f56d930、整合5548744。本地完整顺序count1集成50.431s/race93.834s，独立两轴全部原发现关闭/新增0，fixture架构收益复核闭合；准确CI37162569420 success，完整退出及旧未知schema/CID限制见[证据](../lerna-02-durable-work/exit-evidence.md)。
- 切片 03：**in-progress**。六票42AC中01/04/05的21AC已resolved；规则Decision首票受测源696ac49、交付d806a92，经merger整合5307702，完整tree与worker相同。02候选、03取消/限额与06进程恢复的真实直接前置已满足，待独立工作树实施。新1.1 Decision profile仍未完整广告；整片审查、架构、最终CI与退出待全部六票完成。
- 切片 04–22：**not-started**。04–15仅TMP条件准备，真实前置整片退出后再复核、发布和实施。

## 切片 01 过程检查点（历史记录）

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

09整合检查点 `3a7f1f8` 真实 push [CI 37148346512](https://github.com/ruipengliu/lerna/actions/runs/37148346512) 已 success；两库实际集成4.277s/race10.252s均-count=1，v1校验和不变。

04已 resolved，feature3376e08、准确受测修复ff22936、clean worker c235689经merger合入 `dcb6f44`。PG/SQLite共同13个工作行为、SQLite真实v1来源→v2→原Job Claim完成、UTC固定精度时间边界和关闭重开通过；最终两库integration11.269s、race22.544s通过。一个失败轮的PG cleanup连接已关闭问题修复后，当前成功轮登记scope正常清理；失败轮有一个名称只留在退出进程的schema无法重新确认归属，按安全裁决保留并注明，未按数据形状猜测删除。详见04Comments，尚不声称04远端CI成功。

原八票01/02/03/04完成、额外09完成。05/07/08已 claimed，各从最新集成独立worktree开始：等待/退避采用scheduling-decisions，清理/真实升级采用新[retention-decisions](../lerna-02-durable-work/retention-decisions.md)，故障采用[process-fault-decisions](../lerna-02-durable-work/process-fault-decisions.md)。07的psql17.11完整恢复18.6 dump工具探针已实际通过并清理自己登记scope，不能替代07产品升级/清理验收；08的SQLite存储端口确认丢失不宣称原生COMMIT异常分支。06仍等05；整片退出等待全部核心票、额外修复、审查/架构和真实CI。

## 切片 02 SQLite 接纳票据检查点

票02已 resolved；实际 v1 writer `f4fb057` 在代码提交后从历史 Git archive 构建，完整 SQLite file 与 checksum/provenance 保留。PG与SQLite共同运行13项同版接纳行为；SQLite WAL/FULL运行时核验、Linux跨进程写Host排除/关闭接替、busy、取消、完整Tx关闭生命周期、scope和真实v1文件恢复通过。实际内置SQLite3.53.4/go-sqlite3v1.14.52/CGO，不依赖个人头文件。有限Close失败时保留锁，回调退出后可重试。具体命令与范围见[票02 Comments](../lerna-02-durable-work/issues/02-sqlite-durable-admission.md#comments)。

切片02仍 in-progress；票03 PG Claim已合入，SQLite Claim留给04，调度/SIGKILL/完整升级出口尚未完成。两库必需integration与CI race已加-count=1；缺PG配置/服务或CGO均硬失败。新SQLite远端tip CI待root核实，历史PG成功不替代新两库CI。默认/tmp为tmpfs，实际最终两库integration/race和v1writer另在/workspace下本轮自登记overlayfs临时范围运行；未宣称断电或生产故障域耐久。

04双库领取检查点 `b1674b2` 的准确远端 [CI 37149178541](https://github.com/ruipengliu/lerna/actions/runs/37149178541) 已 success：必需真实集成8.417s/race15.292s均-count=1，原十项v1校验和不变。

05补充采用[legacy策略](../lerna-02-durable-work/legacy-scheduling-decisions.md)及[实际处理门禁](../lerna-02-durable-work/processing-gate-decisions.md)：新接纳及首次接管legacy均绑定5分钟/最多3次启动，重开不刷新；实际Host成功提交必须经过真实Start/权限/期限，旧Complete不得绕过。纯hash及可信runtime存储机制保持各自职责，04/07/08在05整合时更新实际调用与同owner Clock，依赖图不变。07/08本票完成不关闭05或整片02。

08已resolved并经merger合入d89e789；准确push [CI37151050492](https://github.com/ruipengliu/lerna/actions/runs/37151050492) actual success，两库-count=1集成9.718s/race26.419s。SIGKILL提交前/Host回复前、原Claim跨进程接替、SQLite存储端口确认丢失均有正常对照；SQLite原生Commit未知分支与断电不在此证据。

07已resolved，feature4a0871a、工具生命周期修正75a5ac3及最新root整合0ec6386经merger合入0f27475。两库真实v1→v2→0003retention的原回执/原Job继续、版本保存故障回滚/重试、正文清理与独立command gone已通过；mandatory整合24.840s、race49.665s，固定psql18.6工具/历史升级选择race62.746s。CID登记后的真实取消精确清理/absence已验证；未知CREATE无完整ID不启动psql且可能遗留未启动容器，实际失败轮限制保留，不猜删除。详细证据见07Comments。准确新远端CI仍待核验。

原八票现01/02/03/04/07/08完成，额外09完成；05仍实施并承担整合07/08真实Start/统一Clock，06仍等05。06提前采用[容量交接](../lerna-02-durable-work/capacity-handoff.md)：同库同schema/file≤64明确OwnerRef、统一command→pool→input→Job锁序、所有实际消费入口有限耐久pool门禁，历史鉴权查询/原键优先保留，未配置不产生新责任或无限处理；尚不表示06实现。整片仍待05/06、两轴审查、架构优化及真实CI退出。

2026-10-03，票05已resolved，准确受测产品8a1df46（已merge root07/08及418415f）；显式Start/当前资格/immutable revision policy、持久gate wait/有限retry/deadline、原Job丢通知扫描与责任关闭/成功Projection分开在同一真实PG/SQLite suite通过。07/08业务fixture一并真实Start，root已发布v1/v2/v3原迁移及旧夹具保持，新增wait迁移v4。最终mandatory count1 integration26.132s、whole integration-race57.172s/消费者1.658s，fmt/check/test-race/module verify/checksum/diff check均通过；准确red/green、真实v2旧Claim来源、版本/限制见[05 Comments](../lerna-02-durable-work/issues/05-persistent-wait-and-scan.md#comments)。06frontier打开，whole02仍active，容量/配额与后续审查、架构、准确CI未退出。此前05未开始/实施中的记录为历史检查点，不代表当前状态。

07整合准确418415f的[CI37152615344](https://github.com/ruipengliu/lerna/actions/runs/37152615344)已success：固定18.6工具取消race2.378s、真实两库集成11.242s/race28.948s均-count=1，十项v1源hash不变。

05已resolved并经merger合入f28b69d（tested8a1df46/tip887e839）：0004wait、有限policy/legacy首次接管、等待/退避/丢通知、严格Start/Finish及07/08真实处理整合通过两库whole26.132s/race57.172s；真实v2旧writer b1674b2的完整PG dump/SQLite file另行冻结，原v1/已发布001–003不变。根全range diff核验补齐v2原始dump的窄.gitattributes，dump字节不改。06已claimed，按scheduling/capacity-handoff实际入口门禁开始独立worktree实施；原八票只有06尚未完成，额外09已完成。整片退出仍待06、两轴审查、架构优化和准确最终CI。

05整合准确7fa7594的[CI37153111359](https://github.com/ruipengliu/lerna/actions/runs/37153111359)已success：工具race1.567s、两库-count1集成14.404s/race34.816s及27项真实v1/v2来源manifest OK。06额外采用[无执行额度时的到期关闭](../lerna-02-durable-work/capacity-expiry-decisions.md)：可信维护短Tx无需新执行Claim即可按原policy/revision准确关闭到期责任；不算hash/attempt/公平执行机会，不抹新修订或异revision Claim。有限维护服务机会与配置可用前提明确；仅决定，等待真实实施验证。

06继续实施；授权代理根据实际静态索引环的差距采用[动态资格公平次序](../lerna-02-durable-work/fair-eligibility-decisions.md)：每lane持久有界tenant FIFO、已等待者保序、新/恢复合格者入尾、分页解析绑定准确head、成功Claim同Tx移尾，配置变更按身份保序。已通过的固定N=2轮转不替代新增动态资格反例；尾部规则仍待真实两库实施验证。03当前端口复核与04准备仍仅在/tmp，不提前发布或启动依赖切片。

## 切片 02 全部核心票与整片审查

06最终worker cdc7ae6 经merger合入96a0ecc；原八张核心票53条AC与额外09的5条AC均resolved。本地必需两库count1集成60.305s、race91.786s，来源27项hash不变。准确根检查点26c9100的[CI37156883508](https://github.com/ruipengliu/lerna/actions/runs/37156883508)实际success：工具生命周期race1.569s、两库集成21.892s/race47.390s、27项来源校验全部OK。

独立Standards/Spec分别3/1项发现，原报告见[两轴审查](../lerna-02-durable-work/code-review.md)。单一实现分支正在修复全部发现；实际Run固定fallback可能使500ms期限内100ms退避的合法重试错过，PG启动错误也丢失可判断原因。绿色审查前CI不关闭发现。整片02仍等待修复后独立复核、最终架构审查、准确新CI与退出证据；03/04仅/tmp准备未开始。

## 切片 02 审查修复与独立复核

单一修复产品4311585、clean workercc6a053经merger合入6783307；check/base-race、完整count1两库集成50.359s/race103.345s在原timeout120通过，27历史源hash及001–005不变。原Standards三项与Spec一项经同两位独立审查者按完整baseline8e7438e...6783307复核均关闭，新增0项；原因、失败轮诊断与验证细节见[审查记录](../lerna-02-durable-work/code-review.md)。

架构只读探索按6783307最终刷新，保留一个Worth exploring候选：测试fixture稳定scope归属与writer generations。临时HTML已生成，xdg-open因无GUI实际失败；授权代理正在选择/grill，不据此宣称重构已实现。6783307已push，准确[CI37158656088](https://github.com/ruipengliu/lerna/actions/runs/37158656088)已实际success：工具race1.560s、两库集成21.011s/race46.969s，27源hash不变。整片02未退出，03及以后未开始。

## 切片 02 后置 fixture 架构任务

准确6783307只读架构探索保留1 Worth exploring（0 Strong/Speculative）；授权Astra经5轮11项grilling选择现在实施，root采用[决定](../lerna-02-durable-work/architecture-decision.md)并发布唯一[票10](../lerna-02-durable-work/issues/10-owned-fixture-lifetime.md)，目前claimed。稳定fixture handle将统一本轮确证scope归属、writer接替与有限退出；历史来源/故障loader、process Kill/Wait和backend专有故障仍各自负责。待实施后证明普通caller已移除creator保活与指针登记知识，不能只移动helpers/maps。Whole02仍in-progress，03仅准备。

## 切片02架构实施后审查待修复

票10代码1863fc4、clean worker c5d5d40经merger合入c52e68b；完整顺序双库集成52.406s/race93.619s通过，产品/合同/001–005及27来源不变。准确[CI37160694293](https://github.com/ruipengliu/lerna/actions/runs/37160694293)已success。独立收益复核确认稳定fixture handle消除了六类caller及pool的生命周期知识和三张指针maps。两轴各新增1项P2：已确认退出但失败holder的历史错误永久阻断清理。票10重新claimed，原单一review fixer负责全部新增发现，whole02继续in-progress。首版red新遗留两个无法按准确名称确认的PG scopes，与旧04未知schema/07无CID限制独立保留；321PG/319目录absent仅指确证登记项。03/04/05仍仅/tmp准备。

## 切片02正式退出

准确5548744 CI37162569420已success：工具race2.407s、两库count1集成20.985s/race45.463s、27源hash。十票68AC、双轴新增0和唯一架构收益闭合，最后fixer登记285PG/265目录全absent；未知两prototype/旧04schema/07CID限制保留。详见[完整退出](../lerna-02-durable-work/exit-evidence.md)。下一步只在安全确认clean且tip已整合后清理11个02实施工作树、保留分支；03再按最终SHA复核并发布。以下旧章节记录历史阶段，不改写当时失败或状态。

## 切片03启动

02的11个实施工作树均先核对clean且tip为df2dbe5祖先，再安全移除，所有分支保留；仅root工作树留存，未知DB/container不操作。记录/tmp/lerna-02-worktree-cleanup.txt。03最终授权交接见[final-handoff](../lerna-03-deterministic-harness/final-handoff.md)，六张独立issues总42AC，01+04是真正并行frontier；不把schema/表/worker拆成横向半票。实现采用独立owner真实事实、强类型新版codec/账本、actualStart和全members唤醒，独立目标不冒充Executor/Effect。

## 切片03独立目标完成与故障计划启动

04受测cac8e92／clean tip792ee85经merger整合f11135b，6AC resolved。新owner真实SQLite3.53.4/WAL/FULL/FK及0001迁移、普通write/read/query与独立Observer、原键冲突/不续期/准确到期/无query边界通过；final normal0.356s/race1.483s各10项、makecheck/module verify及旧27hash全部通过。Standards两P2及清理followups经原reviewer复核关闭，Spec/Architecture无发现；101个精确登记scope全部absent。证据见[04](../lerna-03-deterministic-harness/issues/04-durable-test-target.md)。不声称供应商、nativeCommit未知、SIGKILL或断电验证，准确新远端CI仍待核验。

05直接前置已满足并claimed，从最新integration新工作树实施耐久fault plan/迟到原效果/seed与cursor；01继续完成自身9AC，02/03等待01，06等待01+05。整个03尚未完成，不提前广告完整decision_engine profile。

04整合检查点270ae4ae8abe2c5245328da2e5c4cdfe514ecf40的准确远端[CI37164714040](https://github.com/ruipengliu/lerna/actions/runs/37164714040)已实际success，两个job所有step通过。基础检查包含新target测试0.087s和旧158共同夹具双向/正反序；既有真实双库恢复count1 normal24.421s/race51.850s、psql18.6工具生命周期race1.550s及27源hash通过。远端race范围为既有recovery，不能当作新target race证据（新target本地race1.483s见04）；未合入的Decision与05fault plan均不在此CI范围。此前“新远端CI待核验”为历史检查点。

## 切片03耐久故障计划完成

05准确受测a0198ed、clean tip e3c3426经独立merger整合为077f61647d484e938402ff14bcaa7d3564abb196，合并tree与worker相同；6AC resolved。私有InstallPlan/RunEvent保存明确步骤、原身份、seed与cursor，ReceiveOnly/ApplyReceived保留迟到责任；实际Rollback与提交后丢响应分别用独立目标事实核验。新增0002实际由冻结04历史writer写出的同一SQLite文件升级，原0001及三项源hash保持。最终19项count1 normal2.264s/race3.615s、bootstrap/check/module verify及旧27hash全部通过；两项Standards P2已复核关闭，Spec/Architecture新增0。120个精确登记scope全部absent，所有sessions结束。准确版本、边界和逐项证据见[05](../lerna-03-deterministic-harness/issues/05-durable-fault-plans.md)。

目前03只完成04/05的12项AC，不提前关闭整片42AC；01继续验证规则启动计量、prepared恢复与真实旧writer迁移，02/03尚未开始，06仍等待01完成。本票未做SIGKILL、原生Commit未知、断电或供应商验证。新push CI尚待按准确head核验；既有04检查点CI不能替代新05代码验证。

05整合检查点682961aecf79d2c2fe08b526e11af225aabfecdf的准确远端[CI37166918488](https://github.com/ruipengliu/lerna/actions/runs/37166918488)已实际success，两个job所有step成功。基础检查包含新target/fault-plan套件1.168s与旧158共同夹具正反序及实际双向往返；既有真实双库恢复count1 normal22.733s/race49.646s、固定psql18.6工具生命周期race2.371s及27来源hash全部OK。该远端race范围仍为旧recovery，新05 target race为本地3.615s；未合入的Decision/0002计量迁移不在此CI。whole03仍in-progress。

## 切片03原Decision规则票完成

01九项AC已resolved；最终源696ac49846105a16f33e5de86dc621a3858651b2、文档交付d806a92eb443b8e0c989cde4e4d7c20c7d61f49c，经merger合入530770274a457356fafe5a2e06832b7ee33f1f2e，双parent和完整tree等于worker已核验。严格1.1 Go/TS、原双身份accepted+Decision+FKJob短Tx、真实独立Source/Publisher、durable Start与完整prepared/原键发布读回/Finish，以及真实旧writer升级通过；[证据](../lerna-03-deterministic-harness/ticket-01-exit-evidence.md)和[实际端口](../lerna-03-deterministic-harness/ticket-01-api-handoff.md)给后票准确接法。

产品机制f96的完整新PG normal25.137/8.208s、race42.337/11.485s，原PG/SQLite恢复normal27.357s/race60.405s均count1/timeout120顺序通过；b43窄历史pipe修复后的真实五状态旧writer升级5.760/9.080s、696测试登记修复后真实pipe0.010/1.058s通过，前41cb makecheck覆盖未变81新+158旧共同字节及JS/生成。两个独立最终轴各0open/0new，46commits/187paths；1074成功ack中的818PG/256FS全部absent，exact空overlayroot rmdir0，所有sessions已退出。历史失败与未知旧scope/CID限制保留，不猜删，也不冒称native driver故障。

本检查点新push/准确远端CI尚待核验，旧05 CI不能替代新Decision。01/04/05合计21/42AC完成不等于whole03；候选全族、取消/资源完整矩阵、SIGKILL仍分别归02/03/06，所有六票完成后root再完成支持清单、架构及整片退出。

## 切片03后续三票实施

准确c9de1ba的[CI37174778053](../lerna-03-deterministic-harness/ci-verification.md)已success：新真实PG Component normal21.061s/race33.892s、Source与真实旧writer升级7.243s/9.385s、原双库recovery23.166s/50.347s及81新/158旧共同双向字节全部通过。前述CI待核验记录为历史。

02/03/06直接依赖已全部resolved，root已正式claimed三个独立票，采用实际首票API及规则兼容、控制、生命周期决定；分别在新worktree/branch按TDD实施。当前已完成仍为01/04/05的21/42AC，不广告完整profile，不关闭whole03。构建与数据库槽独占；新票特殊恢复归自身，全部42AC后root负责整片审查、架构、支持清单、最终CI与安全工作树清理。
