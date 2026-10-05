# 05: 合法正文清理、真实holder残留和孤儿责任

**What to build:** 原正文合法清理后返回gone及可披露最小元数据；真实副本离线/删除失败保留负责方，原身份不复活正文。

**Blocked by:** 02 — 所有来源约束派生内容，撤权阻止新使用

**Status:** resolved

- [x] 受信生命周期seam先封新使用并提交清理责任，不新增公共delete方法；原Command去重/准确版本/最小可披露缺口仍保持。
- [x] 实际删除PG staging和本地对象正文并独立确认后标对应holder erased/gone，不能只改状态；无权主体与获准metadata-only查询区别。
- [x] 真实第二holder实际存过独立字节，离线/删除失败独立观察保留cleanup_pending/residual及负责方/期限，恢复继续原责任，未ACK不报告全局擦除。
- [x] 清理与发布/重传/迟到安装并发有准确原版本关闭及跨进程对象层耐久fence/墓碑，旧worker不能重建gone，不能以Claim/内存mutex代替文件效果隔离。
- [x] 不同新version不受旧清理误删；源政策收紧传播完整有限分页，不只第一页推断无holder。
- [x] 仅对真实登记的未发布对象按精确key/owner/前态做有界孤儿关闭，清理与发布条件竞争不删活引用，不扫描他人scope。
- [x] 正常/失败/重开通过Content、受信holder和独立字节观察验收；WAL/备份法证擦除、已披露字节撤回、生产多机保证不在当前证据内。

## Comments

2026-10-04，按用户授权与最终API复核发布；前置03完整退出5fbb1a0，采用decisions/final-api-handoff的具体映射。本票独立垂直出口，不将全片广告/审查/CI作为隐藏关闭依赖。


2026-10-04，02七AC已resolved并合入6f88740，验收检查点904ec5f实际push成功。root复核当前Content产品/端口/两迁移/API与固定651036字节相等，采用条件handoff及最终端口差量，正式claimed本票；新准确CI37227467190待核，不将整片CI作为本票隐藏阻塞。独立WT实施，LOCAL native槽须逐次明确授予，不与另一票并发。


## Answer

2026-10-05，root已正式接受源码 `8db74e3dfd2677adb37fbdd13bd93e76b61b5eb7` 的七项AC，核定文档提交 `94e9c289310601bbf1eab5580efe002028db0cc9`。本票本机真实PG/Linux范围的逐AC行为、当前27项实际检查、独立双轴与资源观察见 [root退出证据](../ticket-05-exit-evidence.md)，当前接口及限制见 [交接](../ticket-05-api-handoff.md)。两个历史logical Close UNKNOWN保持原责任与inode，不因当前资源不存在改写关闭事实。

worker已按implement-spec要求正常merge准确integration root94e9至自身分支，merge `30cb0c0c388477a1a2ba0c0ac0bc776f224ed4dc` 的parents为原受测8db与root94e9；15份已资格源码／SQL／test字节保持8db原pin，无新增产品修正或native测试。七项勾选与resolved表示root已接受的本票资格；实际交付回integration、push及新准确CI尚待root后序记录。worker本地票状态累计22/41，whole04仍in-progress、完整1.2广告OFF。票06仅依赖本票实际交付，不增加整片CI隐藏依赖。

2026-10-05，实际整合merge361c353完成；root独立核parents／整树／15pin。前文待交付为worker历史cutoff，本票现已交付。票06直接前置满足，whole04累计22/41，整片广告及新准确CI仍未接受。
