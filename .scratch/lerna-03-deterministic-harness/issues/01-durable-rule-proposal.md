# 01: 原 Decision 的耐久规则提案

**What to build:** 组件开发者用固定输入提交一次规则 Decision，得到固定接纳事实和可恢复的 Proposal／必要产物；原身份重传不会创建第二份工作。

**Blocked by:** 切片02完整退出（已满足，最终代码5548744／退出文档df2dbe5及端口复核见最终交接；本片内无前置票）

**Status:** claimed

- [ ] 新增准确1.1.0类型与真实编解码路径，旧1.0.0源、方法、黄金和公开行为不扩张；Go contract/v1_1及TS ./v1_1隔离新版闭合输入输出、Schema及缓存，完整可达输入／输出Schema摘要与双语言原字节往返、严格拒绝共同验证。未完整的decision_engine profile不广告，完整清单由root在全票退出整合时开放。
- [ ] 明确耐久fixture dispatcher／source／publisher及实际owner，固定Snapshot、Task场景身份、准确材料、组件fixture lock和规则版本；不声称真实Task、ContextCompiler、Content或生产安装已实现，重开仍能按原引用读出字节。
- [ ] decide经当前受信主体、用途、准确owner和版本验证；首次Decision、固定accepted回执及必要Job同本owner有限短事务提交，不在持锁事务中等待其他owner发布或读取。
- [ ] Command原键摘要及Decision输入摘要分别按已批准域和字段绑定；原命令重传返回原事实，新命令同Decision同输入只关联原Decision，异输入稳定decision_mismatch，不创建第二Job或改原记录。
- [ ] 一个正常规则候选从准确输入取得可预测Proposal；发布身份与内容摘要固定，先耐久发布并读回必要输出，再保存完整来源和completed；发布与Decision提交是可恢复交接，不伪称跨owner原子事务。
- [ ] decision_engine.get返回准确Decision、Proposal、必要引用与fixture用量，当前进展不改原accepted；查询鉴权、保持原owner且只读，不重新读取同名最新Snapshot替换固定输入。未找到与读取不可用有不同闭合结果；状态变体预留准确cancelled关闭绑定，不强制尚未到达的decide请求／Snapshot存在，取消行为由03交付。
- [ ] 新版command.get准确返回新版固定回执；旧版可无损表示的原事实仍可查，不可表示的新记录沿既有unavailable而不改reason、强cast或推断未发生。
- [ ] 真实PG验证正常、回滚、重开、并发双身份去重及跨tenant／owner拒绝，迁移版本化、checksum和受影响旧版升级明确；复用02实际机制，不凭接口名称宣称正确。
- [ ] 锁定来源和版本，全部新增数据库I/O及fixture交接有限；缺服务／配置硬失败，原1.0套件、新版共享夹具及构建继续通过。只记录本票证据，不提前宣称取消、全部候选或整片完成。

## 最终接法注记（whole02已退出）

采用 `../final-handoff.md`：准确代码/CI为554874470d5abeb71fa743708580f3121b8944f1，正式退出文档为df2dbe5624120bc258dc7419ea022a08ebc0d6b0；whole02及发布前端口小复核现已满足。首票即需独立PG Decision FK/新版账本、当前有限pool/queue准入和真实Start/Finish门禁；等待覆盖准确pool全部登记成员的due、lease及未关闭执行期限，不仅anchor，quota满/0仍有有界本地维护且不忙转。正常多member/非anchor早到期观察随原第8/9条交付。机制值/纯FIFO按实际第二consumer抽取，组件声明小端口；不复制demo Store、不伪造Input、不改0001–0005或旧1.0。没有新增AC或依赖；root负责正式发布和分配。


## Comments

2026-10-03，root依据授权Astra批准的粒度/真实edges及最终df2dbe5前置退出发布；本票验收尚未实现。已claimed，交独立工作树实施。

2026-10-04，实施检查点：完整规则 tracer 已真实 PG 接纳、重开两个 owner、领取原 Job、Start、读取原来源、固定键发布/独立读回、Finish、get 与原 accepted command.get。首 red 为471dfe0缺组件；来源严格 Revision red0.192s→green0.966s。后续 durable-start/完整 prepared/累计未知窗口按已采用 decisions §10 实施，原 fixture-rule/1 不改含义；0002只分类，当前 owner 的 Maintenance 正常事务关闭旧 active 工作。冻结970fd90的68原源码+1原创 driver 共430396字节，正常验收从已提交准确来源构建，不依历史 Git 对象。

root 两轴预审的五项 P2 已统一修复：原输入及完整 delegated Subject 对 Authority 隔离且返回 Permission 深拷贝，所有实际 Authorize 入口共用此规则；当前鉴权后原 Command 键先返回固定事实，配置变更仅影响新请求；0002分类保持原业务时间/状态，正常 Maintenance 关闭；peer与失败 Open 的未确认 Close 保留 handle；新版合同/生成器有界异步构建、组终止/退出确认、所有原失败及 Close 错误聚合、未确认范围保留。身份/配置真实 red0.782s→green1.101s；delegated 初测试重用 immutable fixture identity 导致 conflict0.419s，修正新准确 Decision 后 green0.882s；全部输入修改用途（含 worker Start）最新 green1.173s。升级测试新 command.get Target 尚指向旧 Command，真实 red8.811s/诊断9.806s，修正准确 Target 后 green11.201s、清理组确认最新 green10.558s（accepted、Start前计算 running、completed、真实 cost-limit failed、两次真实发布后未 Finish running）。

构建管道风险进一步从 spawnSync 改成 async spawn：截止立即杀已知进程组，输出/Close/组 absence 有界确认。真实继承 stdout/stderr descendant 由独立 Linux subreaper/watchdog 回收；4专项及原16 runner 测试 green4.957s，新版 generator拒绝/摘要及81共享 typed 双向往返 green。相关 PG Component focused green6.618s；prepared interrupted-window red2.308s 原因是测试将尚未发布引用错误期望为 unavailable，而真实 reader 正确拒绝无 publication permission，校正后 green7.202s。全部 session 已退出。以上为检查点；最终准确 pin 的 make check、race、新旧真实数据库顺序回归、精确资源核对及两个独立最终复核尚待完成，9AC仍未勾选或 resolved，不宣称后票/整片完成。
