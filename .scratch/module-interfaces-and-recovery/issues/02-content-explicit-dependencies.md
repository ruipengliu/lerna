# 02: 内容登记、正文与观察使用完整依赖

**What to build:** 有效内容 Adapter 通过声明的 Interface 完成登记、发布、读取、派生、观察及文件资源处理，不在业务调用中发现隐藏能力。

**Blocked by:** None (can start immediately).

**Status:** resolved

- [x] 内容治理声明所有必需 Store、Work 和关联事实能力，取消必需能力的运行时断言。
- [x] 生产 SQLite 及内容域 Work 通过消费方 Interface 的编译验证；基础构造缺项明确拒绝，循环链接保留显式完成检查。
- [x] 通过生产宿主命令和查询验证登记、正文发布、读取、派生、观察与 FILE 资源，字节、来源、版本、摘要和原回执保持。
- [x] 正文持有方接纳与源方确认的提交仍分别保存；失败、回执丢失和重放不制造第二正文或替代观察。
- [x] 无新增 schema、迁移、正文读取授权或用途放宽；相关设计先于代码更新。
- [x] 相关内容与故障场景通过，记录受测源码和命令结果。

## Comments

2026-10-07: Claimed for implementation on codex/interfaces-02.

## Answer

2026-10-07: 内容 `Store` 组合登记与正文、派生、观察和 FILE 资源的全部现有持久能力，业务路径改为直接调用声明的方法。`New(Store, ObservationWork, user, domain)` 返回 Service 与错误，拒绝 Store/Work 的 nil 和 typed nil；`ValidateDependencies` 检查关联事实与执行管理等全部连接。宿主先建立内容域 Work，保留两阶段循环连接，并在保存版本检查和恢复前验证内容配置。共享必需依赖校验器与 06 的实现一致，静态依赖许可只增加精确的 `core/durable$`。

相关内容设计先更新于 `docs/architecture/core/content/README.md`。生产 Store、内容域 Work 与命令 Adapter 均通过消费方 Interface 的编译检查；只暴露声明方法的替换 Store 测试先复现隐藏 `AcceptContentBody` panic，再通过正文发布、来源与原回执验证。旧最小 Adapter 的独立编译探针现在明确失败。正文持有方、源方确认、原观察和文件资源的提交边界、身份及权限保持；未更改协议、schema 或迁移。

验证源为 `92dc68cacb19d96bfdae51d9d919f99c567f30b9`。原内容基线与三轮 red/green、`make check-code CHECK_PACKAGES='./core/content ./core/durable ./cmd/assembly ./conformance/content'`、`make check-docs`、`git diff --check` 均通过。同步集成分支后的未缓存观察／原字节切片通过（5.545s）；登记和派生崩溃矩阵、正文持有方回执丢失、FILE 丢观察、失败资源保留／清理与根绑定提交边界的未缓存 fault 切片通过（43.369s）。完整命令与证据见 `/tmp/lerna-module-interfaces-implementation/ticket-02.md`、`ticket-02-final-check.log`、`ticket-02-final-observation.log`、`ticket-02-final-fault.log`，编译与 red 证据同目录保留。

合并前核对待提交源码与上述验证源一致，无冲突，复用已有检查。本次仅增加本工单验收状态与 Answer；完整集成检查由终点验收执行，父 spec 保持原文与状态。
