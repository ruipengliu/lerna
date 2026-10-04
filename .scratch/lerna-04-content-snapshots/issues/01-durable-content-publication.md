# 01: 原内容版本的耐久发布与准确读取

**What to build:** 调用方提交有限正确字节取得固定接纳，之后按原准确引用读回完全相同的已发布字节；错误声明和版本冲突不会生成另一版本。

**Blocked by:** None（01–03整片退出及实际API复核已满足）

**Status:** claimed

- [ ] 准确1.2.0闭合Content put/get及新Command读取合同、Go/TS隔离类型/编解码/共同黄金随完整正常链交付；1.0/1.1方法、Schema、拒绝及旧黄金冻结，未完整profile不广告。
- [ ] 真实PG Content owner在一短Tx提交有界staging、准确声明、固定accepted和必要Job；上传不在持锁Tx，accepted与published明确区分。
- [ ] 独立真实本地对象adapter执行有界临时写、原字节hash/length核验、文件Sync、不覆盖安装、目录Sync及独立回读后才确认发布；失败不报告可读published。
- [ ] 原Command键/摘要幂等与准确Content版本身份分别去重；同版本不能换字节/来源/用途，异声明version_conflict，原回执及责任优先于新尝试。
- [ ] 严格canonical padded base64、256KiB解码正文/1MiB总包、64有限sources、准确hash/byte_length、版本、空内容及有限range正反例；拒绝越界、未知/重复字段，不截断。
- [ ] 显式耐久read/process/save测试政策、受信主体/owner、有限限额/截止从首次开放起有效，缺配置硬失败；不是允许全部或生产Grant，跨租户拒绝。
- [ ] 准确put/get/Command事实及真实PG+对象重开恢复正常完成，缺失/损坏字节返回integrity/unavailable；查询不修补或另找latest，旧reader仅无损桥接否则保原unavailable。
- [ ] 新owner真实空库初始化、重复/checksum及现有受影响旧路径验证，1.0/1.1已发布SQL/archive保持；登记确切scope并在native退出后安全清理，公开业务验收不靠私表/调用次数。

## Comments

2026-10-04，按用户授权与最终API复核发布；前置03完整退出5fbb1a0，采用decisions/final-api-handoff的具体映射。本票独立垂直出口，不将全片广告/审查/CI作为隐藏关闭依赖。
