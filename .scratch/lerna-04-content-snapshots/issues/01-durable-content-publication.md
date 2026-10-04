# 01: 原内容版本的耐久发布与准确读取

**What to build:** 调用方提交有限正确字节取得固定接纳，之后按原准确引用读回完全相同的已发布字节；错误声明和版本冲突不会生成另一版本。

**Blocked by:** None（01–03整片退出及实际API复核已满足）

**Status:** resolved

- [x] 准确1.2.0闭合Content put/get及新Command读取合同、Go/TS隔离类型/编解码/共同黄金随完整正常链交付；1.0/1.1方法、Schema、拒绝及旧黄金冻结，未完整profile不广告。
- [x] 真实PG Content owner在一短Tx提交有界staging、准确声明、固定accepted和必要Job；上传不在持锁Tx，accepted与published明确区分。
- [x] 独立真实本地对象adapter执行有界临时写、原字节hash/length核验、文件Sync、不覆盖安装、目录Sync及独立回读后才确认发布；失败不报告可读published。
- [x] 原Command键/摘要幂等与准确Content版本身份分别去重；同版本不能换字节/来源/用途，异声明version_conflict，原回执及责任优先于新尝试。
- [x] 严格canonical padded base64、256KiB解码正文/1MiB总包、64有限sources、准确hash/byte_length、版本、空内容及有限range正反例；拒绝越界、未知/重复字段，不截断。
- [x] 显式耐久read/process/save测试政策、受信主体/owner、有限限额/截止从首次开放起有效，缺配置硬失败；不是允许全部或生产Grant，跨租户拒绝。
- [x] 准确put/get/Command事实及真实PG+对象重开恢复正常完成，缺失/损坏字节返回integrity/unavailable；查询不修补或另找latest，旧reader仅无损桥接否则保原unavailable。
- [x] 新owner真实空库初始化、重复/checksum及现有受影响旧路径验证，1.0/1.1已发布SQL/archive保持；登记确切scope并在native退出后安全清理，公开业务验收不靠私表/调用次数。

## Comments

2026-10-04，按用户授权与最终API复核发布；前置03完整退出5fbb1a0，采用decisions/final-api-handoff的具体映射。本票独立垂直出口，不将全片广告/审查/CI作为隐藏关闭依赖。

## Answer

真实首次垂直链及全部8项的实现和执行证据已形成，仍待root独立两轴接受后勾选。
隔离1.2 Go/TS/共同黄金、Content domain、consumer-owned PG短Tx staging+固定receipt+
Job、Linux真实Sync/noclobber/dirSync/独立回读、真实重开及公开Content/Command正常和
拒绝对照已交付。当前read/disclose双门禁、已登记DIRECT来源、同声明新关联和原Command
锁后reader复核均按已采用决定实现；完整inherited closure等后票义务未伪称完成。

源码修复固定于`46d6ca2`；实际API见[../ticket-01-api-handoff.md](../ticket-01-api-handoff.md)，
执行与初始失败分类见[../ticket-01-evidence.md](../ticket-01-evidence.md)，
准确资源退出见[../ticket-01-resource-audit.json](../ticket-01-resource-audit.json)。
独立初审原pin及findings被保留，最终root接受和exact delivery pin随后追加。

后续固定72候选的两轴/Astra复核发现target完整policy绑定P2，已以公开真实red→green及
新门禁顺序补充反例修复。新受测源码`14ead831b8834892da5ff13a9b883494247f327f`，
之后`4aceecefb45125597d45db75aa757fe1aab96eb9`仅改两行旧顺序注释，执行代码/测试
数据等价，未为注释重跑native。当前完整locked check、Get影响race及fresh资源审计已真实
通过；新机器记录见[../ticket-01-resource-audit-target-policy.json](../ticket-01-resource-audit-target-policy.json)。
原72 findings、初始不充分after-mismatch建议和对应实际green历史均保留，新candidate仍待
root最终独立source/doc qualification接受，当前不勾8项或写resolved。

2026-10-04，root按最终1a7独立两轴/架构/实际执行及资源资格接受8/8AC，正式合并eba167d；详细记录见[首票退出](../ticket-01-exit-evidence.md)。以上pending为历史审阅阶段，当前已resolved；04整片及其余33AC继续实施。
