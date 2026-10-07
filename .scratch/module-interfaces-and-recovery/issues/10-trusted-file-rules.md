# 10: FILE 证据与原版本查询使用受信规则 Adapter

**What to build:** 文件发布、读取、清理及原尝试查询使用既有受信规则 Interface，原发布责任和终局依据保持。

**Blocked by:** 09（API 执行与查询使用受信规则 Adapter）.

**Status:** resolved

- [x] FILE 原动作及 QUERY 规则迁移到受信 Adapter，并删除对应旧规则调用路径。
- [x] 核心继续持有发布历史关联、效果投影与资源责任，固定规则身份和文件版本引用保持。
- [x] 通过公共 CREATE、REPLACE、READ、CLEANUP、关闭后核对与原版本查询验证真实文件及证据。
- [x] 屏障失败、读回失败及前驱证据负例保持各阶段原有 UNKNOWN、NOT_APPLIED 与迟到结论；失败不得声明持久成功，内容匹配或根绑定不替代发布证明。
- [x] 只读查询不重放旧发布、不覆盖后继文件、不改变固定 Result；零费用仍使用原费用证据。
- [x] 纯资格检查、历史兼容与独立文件事件计数不变，设计及相关普通与故障场景通过。

## Comments

2026-10-07: Claimed after ticket 09 resolved, on codex/interfaces-10.

## Answer

2026-10-07: `infra/rules.File` 使用既有 `EvidenceRules` 解释受管理 FILE 的发布、读取、清理和独立 QUERY；宿主注入编译进的固定 API／FILE 清单 `rules.Fixed`，无注册机制或未知版本回退。对应核心终局解释与 FILE 查询解析路径已删除。`QueryFilePublication`、原发布历史归因、效果投影、资源资格和核心写入责任保留；两侧只共用 `contracts/command` 的最小纯观察／提交绑定比较，不执行 I/O、存储或终局裁决，不重解释旧 Effect。`managed-file-v1`、`reference-query-v1`、`reference-query-subject-v1` 及原文件版本、发送和查询主体绑定保持，查询自身完成与原主体证据仍分别处理。纯资格检查核验原固定声明，完整保存描述和关系继续由原编译器核验，不读取根或正文、不重编或重新发布。相关执行管理和文件设计在代码前更新，无 schema、协议或持久格式变化。

公共 FILE 基线和真实原生负例基线通过。新增配置测试先复现缺失 FILE 规则仍被接受的 red，再验证 nil／typed nil 拒绝时原 Operation 与目标计数不变，支持原固定规则时无需文件根即可完成纯资格检查。实际 CREATE／REPLACE／READ、短写和各屏障／读回失败、前驱证据缺口、接受历史查询、弱证据、关闭后的原版本查询及固定 Result、未知旧版本和执行器一致性切片通过；未放宽各阶段 UNKNOWN、NOT_APPLIED 或迟到判据，原逐发送零费用证据仍独立核验。合并核对另运行既有实际孤立资源清理、未知／变更资源与清理屏障失败、当前发布保护、跨目标读取和孤立对象查询五项 fault+race 切片，全部通过（14.098s），补齐实际 CLEANUP 与绑定负例的证据。

最终分支源 `4a720bfd36c921868618c939e796898ed85f06e7` 已同步集成 `3f15541730c22d55297d5c490ad3203434ef36d9`。行为验证源 `b4e1f0f` 与最终源仅有 06／07 工单状态差异；合并后的公共 FILE／配置 race（20.182s），原生屏障、前驱、原版本／关闭查询、兼容与执行器 fault+race（36.193s），`make check-code CHECK_PACKAGES='./contracts/command ./core/ledger ./infra/rules ./cmd/assembly'`、双 lint、规则、格式和 scoped race、文档检查均通过。API 原终局、原尝试查询与生产 Driver 回归亦通过（30.334s）。源码、命令和 baseline/red/green 记录见 `/tmp/lerna-module-interfaces-implementation/ticket-10.md` 及同目录日志；追加清理核对为 `ticket-10-merger-cleanup.log`。

一次过宽的 `TestNativeFile*` 匹配额外启动三组完整存储协议／负对照／时序矩阵，已按开发验收流程停止（exit 143），该轮未完成，**不计为通过**；记录保留为 `ticket-10-broad-fault-stopped.log`。这些完整矩阵由最终评审后集成候选的 `make check` 统一验收。本次合并的源码树与最终分支树 `8ce0befcf60663a35edfa7a45ebc1ebf39a96295` 完全一致，复用已有源码检查；只额外记录本工单验收状态和 Answer。父 spec 与三个原有用户编辑保持不变。
