# 01: 原 Decision 的耐久规则提案

**What to build:** 组件开发者用固定输入提交一次规则 Decision，得到固定接纳事实和可恢复的 Proposal／必要产物；原身份重传不会创建第二份工作。

**Blocked by:** 切片02完整退出（已满足，最终代码5548744／退出文档df2dbe5及端口复核见最终交接；本片内无前置票）

**Status:** resolved

- [x] 新增准确1.1.0类型与真实编解码路径，旧1.0.0源、方法、黄金和公开行为不扩张；Go contract/v1_1及TS ./v1_1隔离新版闭合输入输出、Schema及缓存，完整可达输入／输出Schema摘要与双语言原字节往返、严格拒绝共同验证。未完整的decision_engine profile不广告，完整清单由root在全票退出整合时开放。
- [x] 明确耐久fixture dispatcher／source／publisher及实际owner，固定Snapshot、Task场景身份、准确材料、组件fixture lock和规则版本；不声称真实Task、ContextCompiler、Content或生产安装已实现，重开仍能按原引用读出字节。
- [x] decide经当前受信主体、用途、准确owner和版本验证；首次Decision、固定accepted回执及必要Job同本owner有限短事务提交，不在持锁事务中等待其他owner发布或读取。
- [x] Command原键摘要及Decision输入摘要分别按已批准域和字段绑定；原命令重传返回原事实，新命令同Decision同输入只关联原Decision，异输入稳定decision_mismatch，不创建第二Job或改原记录。
- [x] 一个正常规则候选从准确输入取得可预测Proposal；发布身份与内容摘要固定，先耐久发布并读回必要输出，再保存完整来源和completed；发布与Decision提交是可恢复交接，不伪称跨owner原子事务。
- [x] decision_engine.get返回准确Decision、Proposal、必要引用与fixture用量，当前进展不改原accepted；查询鉴权、保持原owner且只读，不重新读取同名最新Snapshot替换固定输入。未找到与读取不可用有不同闭合结果；状态变体预留准确cancelled关闭绑定，不强制尚未到达的decide请求／Snapshot存在，取消行为由03交付。
- [x] 新版command.get准确返回新版固定回执；旧版可无损表示的原事实仍可查，不可表示的新记录沿既有unavailable而不改reason、强cast或推断未发生。
- [x] 真实PG验证正常、回滚、重开、并发双身份去重及跨tenant／owner拒绝，迁移版本化、checksum和受影响旧版升级明确；复用02实际机制，不凭接口名称宣称正确。
- [x] 锁定来源和版本，全部新增数据库I/O及fixture交接有限；缺服务／配置硬失败，原1.0套件、新版共享夹具及构建继续通过。只记录本票证据，不提前宣称取消、全部候选或整片完成。

## 最终接法注记（whole02已退出）

采用 `../final-handoff.md`：准确代码/CI为554874470d5abeb71fa743708580f3121b8944f1，正式退出文档为df2dbe5624120bc258dc7419ea022a08ebc0d6b0；whole02及发布前端口小复核现已满足。首票即需独立PG Decision FK/新版账本、当前有限pool/queue准入和真实Start/Finish门禁；等待覆盖准确pool全部登记成员的due、lease及未关闭执行期限，不仅anchor，quota满/0仍有有界本地维护且不忙转。正常多member/非anchor早到期观察随原第8/9条交付。机制值/纯FIFO按实际第二consumer抽取，组件声明小端口；不复制demo Store、不伪造Input、不改0001–0005或旧1.0。没有新增AC或依赖；root负责正式发布和分配。


## Comments

2026-10-03，root依据授权Astra批准的粒度/真实edges及最终df2dbe5前置退出发布；本票验收尚未实现。已claimed，交独立工作树实施。

2026-10-04，实施检查点：完整规则 tracer 已真实 PG 接纳、重开两个 owner、领取原 Job、Start、读取原来源、固定键发布/独立读回、Finish、get 与原 accepted command.get。首 red 为471dfe0缺组件；来源严格 Revision red0.192s→green0.966s。后续 durable-start/完整 prepared/累计未知窗口按已采用 decisions §10 实施，原 fixture-rule/1 不改含义；0002只分类，当前 owner 的 Maintenance 正常事务关闭旧 active 工作。冻结970fd90的68原源码+1原创 driver 共430396字节，正常验收从已提交准确来源构建，不依历史 Git 对象。

root 两轴预审的五项 P2 已统一修复：原输入及完整 delegated Subject 对 Authority 隔离且返回 Permission 深拷贝，所有实际 Authorize 入口共用此规则；当前鉴权后原 Command 键先返回固定事实，配置变更仅影响新请求；0002分类保持原业务时间/状态，正常 Maintenance 关闭；peer与失败 Open 的未确认 Close 保留 handle；新版合同/生成器有界异步构建、组终止/退出确认、所有原失败及 Close 错误聚合、未确认范围保留。身份/配置真实 red0.782s→green1.101s；delegated 初测试重用 immutable fixture identity 导致 conflict0.419s，修正新准确 Decision 后 green0.882s；全部输入修改用途（含 worker Start）最新 green1.173s。升级测试新 command.get Target 尚指向旧 Command，真实 red8.811s/诊断9.806s，修正准确 Target 后 green11.201s、清理组确认最新 green10.558s（accepted、Start前计算 running、completed、真实 cost-limit failed、两次真实发布后未 Finish running）。

构建管道风险进一步从 spawnSync 改成 async spawn：截止立即杀已知进程组，输出/Close/组 absence 有界确认。真实继承 stdout/stderr descendant 由独立 Linux subreaper/watchdog 回收；4专项及原16 runner 测试 green4.957s，新版 generator拒绝/摘要及81共享 typed 双向往返 green。相关 PG Component focused green6.618s；prepared interrupted-window red2.308s 原因是测试将尚未发布引用错误期望为 unavailable，而真实 reader 正确拒绝无 publication permission，校正后 green7.202s。全部 session 已退出。以上为检查点；最终准确 pin 的 make check、race、新旧真实数据库顺序回归、精确资源核对及两个独立最终复核尚待完成，9AC仍未勾选或 resolved，不宣称后票/整片完成。

2026-10-04，最终源/测试固定为696ac49846105a16f33e5de86dc621a3858651b2；产品/机制最后变更f96f85987818a5e8dc5c0c104e5731ddf75687df，实际旧writer/prestart helper为b43ceba88eafba323abb7ee5eb95302e88e8c971，696仅测试立即登记cleanup；已在 clean 协调点合并最新 integration 9a06bc1（合并提交2e77f4f），当前 root 未变。Standards 最终发现的可移植旧 writer 构建范围、失败重开 holder、Mkdir 后立即精确清理以及全部选定原错误原因出口，统一在255fd4a/b1e631e/f79af98/f96f859修复。root FULL-read采用 PG/SQLite 生命周期窄决定：PG 首次 Close 失败永久保持 unknown、并发等待原结果，启动双失败返回实际 cleanup-only holder 和两个安全包装原因；SQLite native Close 失败不 release writer，release失败亦 sticky，native之前的排空超时仍可在实际退出后重试。全部实际 opener 先保留 concrete holder 再处理错误，避免 typed nil；不修改冻结归档或业务合同。

机械 database/sql.OpenDB 测试无物理scope：first-error/concurrent-early-return/错误release 真实 red0.010/0.014/0.007s→基础green0.072/0.064/0.012s；扩展启动/取消/双原因/release matrix 首次编译失败为测试helper遗漏CloseDone，修正normal0.061/0.075/0.008s、race1.105/1.162/1.056s。不冒充真实pgx/sqlite3 native Close故障。tagged全包vet初次发现原pool_wake重新赋值cancel的闭包路径警告，补显式defer cancel后0；build/gofmt/diff-check0。

准确 f96f859 全部必要实际回归顺序执行，原 count=1/timeout=120s，无 skip 或延期：`go test -race -count=1 -timeout=120s ./...`通过；`go test -p=1 [-race] -tags=integration -count=1 -timeout=120s ./conformance/component ./conformance/internal/decisionfixture` normal25.137/8.208s、race42.337/11.485s；`go test [-race] -tags=integration -count=1 -timeout=120s ./conformance/recovery/...` 原PG/SQLite normal27.357s、race60.405s。包括8组采用的基本Start/prepared/accounting边界、全部来源与真实旧writer升级、原两库实际恢复/排空/接替/隔离。41cbf7c `make check`通过包含未变JS/schema/generated的81新版+158旧版Go↔TS双序原字节往返、TS37及基础设施20测试；后来纯Go变动由最新vet/build/base-race/全部实际DB回归覆盖，按root协调不重复无关JS。模块全验证；旧27hash、冻结69payload的71closure entries全验证；旧1.0及host0001–0005、两个新owner0001的准确源diff为0。

本地环境 Go1.27.1/Node24.19.0/pnpm12.8.1/PG18.6/psql17.11、Linux/CGO；DSN仅程序读取并经环境注入，专用database lerna_durable_work_01_20261003。所有会话已实际退出。成功CREATE/Mkdir ledger1074条：818唯一PG与256唯一文件范围全部absent；自身empty overlay TMPDIR根仅exact rmdir已0；仅查询已登记准确名字，不查删未知旧范围。第一次只读psql审计fullURI PGDATABASE未展开导致失败，无mutation；转换为安全libpq环境字段后审计0。准确证据 /tmp/lerna-03-ticket-01-evidence.md、final-review-input.md、cleanup-audit.json及五份 final-*-f96f859.log；候选/取消/完整SIGKILL/生产provider/全片退出仍由后票负责。

最后同finding2 prestart pipe端点遗漏真实red0.011→三测试0.014/race1.047绿，实际b43旧writer五状态升级normal5.760/race9.080退出0；测试返回cleanup先登记后检查的残留于696单文件修复，三purepipe normal0.010/race1.058、tagged包vet0。root FULL-read采用两个696最终独立报告：Standards0open/0new，Spec a/b/c各0，范围46commits187paths。全部9AC已准予resolved，只新增本票tracked退出文档；产品不再改动。准确证据[exit evidence](../ticket-01-exit-evidence.md)、[实际API](../ticket-01-api-handoff.md)、[Standards](../ticket-01-standards-review.md)、[Spec](../ticket-01-spec-review.md)。

## Answer

交付完整原Decision规则Proposal纵向链；九项AC全部通过，准确最终源696ac49，执行/审查分层pin及失败历史见[退出证据](../ticket-01-exit-evidence.md)。

| AC | 已验证行为与来源 |
| --- | --- |
| 1 | 独立严格1.1Go/TS/schema/cache/双向原字节，旧1.0不扩；makecheck81新+158旧双序，Decision profile不广告 |
| 2 | 实际独立Source/Publisher owner、固定Snapshot/material/lock/manifest，来源真实重开/授权/有限字节 |
| 3 | 当前主体/用途/owner/版本，accepted+Decision+真实FKJob同短Tx；公开admission/rollback/token测试 |
| 4 | Command+Decision双identity/digest、并发fixedreceipt、新key同input无新Job与mismatch；identity测试 |
| 5 | 一正常候选、完整prepared、原keys/digests/refs实际耐久发布+独立读回再Finish；main/accounting/handoff/Tx边界 |
| 6 | get准确state/Proposal/source/usage及只读原input，未找到与closedDB不可用不同；read_finish/admission；cancel形状无假Snapshot |
| 7 | 真实1.1command.get固定accepted不随进度变；旧只无损投影，不可表示沿原unavailable |
| 8 | 真实PG normal/race：双identity/auth/rollback/恢复、allmember有限pool、0002分类+真实旧writer正常维护升级，原双库完整回归 |
| 9 | 有限预算/期限/I/O/共享mechanics与独立精确cleanup；模块/27旧hash/69旧payload、Makefile/CI新两PG包、两独立轴0 |

本票不声明候选全族、取消、完整SIGKILL、生产provider或整片03退出。Root负责正式integration合入、push/远端CI与整片发布。
