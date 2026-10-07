# 02: 内容登记、正文与观察使用完整依赖

**What to build:** 有效内容 Adapter 通过声明的 Interface 完成登记、发布、读取、派生、观察及文件资源处理，不在业务调用中发现隐藏能力。

**Blocked by:** None (can start immediately).

**Status:** claimed

- [ ] 内容治理声明所有必需 Store、Work 和关联事实能力，取消必需能力的运行时断言。
- [ ] 生产 SQLite 及内容域 Work 通过消费方 Interface 的编译验证；基础构造缺项明确拒绝，循环链接保留显式完成检查。
- [ ] 通过生产宿主命令和查询验证登记、正文发布、读取、派生、观察与 FILE 资源，字节、来源、版本、摘要和原回执保持。
- [ ] 正文持有方接纳与源方确认的提交仍分别保存；失败、回执丢失和重放不制造第二正文或替代观察。
- [ ] 无新增 schema、迁移、正文读取授权或用途放宽；相关设计先于代码更新。
- [ ] 相关内容与故障场景通过，记录受测源码和命令结果。

## Comments

2026-10-07: Claimed for implementation on codex/interfaces-02.
