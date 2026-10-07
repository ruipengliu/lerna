# 08: 生产装配完成后才允许资格检查与恢复

**What to build:** 宿主在任何业务恢复或目标访问之前拒绝缺项装配；完整装配继续正常打开并处理原责任。

**Blocked by:** 02（内容登记、正文与观察使用完整依赖）、03（运行记录收集、查询与指标使用完整依赖）、04（输入、确认与授权链声明完整依赖）、05（任务裁决与持久工作声明完整依赖）、06（动作执行与核对声明完整依赖）、07（逐发送费用与结算声明完整依赖）.

**Status:** resolved

- [x] 汇总各 Module 的完成检查，保留真实循环的两阶段连接；只有完整连接才返回可用生产 Harness。
- [x] 缺失必需依赖、nil、typed nil 与关键循环缺链产生具体装配错误，不留到首个业务命令。
- [x] 缺项场景中各业务 owner 的原实现/历史资格检查、业务恢复及独立目标访问次数均为零；存储打开前仍按原契约核验物理格式和身份；完整装配可正常读取并推进原记录。
- [x] 每个 owner 已声明所有支持路径必需能力；收缩本批迁移留下的无调用旧构造/连接入口，不删持久记录。
- [x] 装配仍只连接具体实现，业务裁决归各原 owner；不改变消息、schema、迁移摘要、格式身份及支持版本。
- [x] 设计与装配测试更新，原启动、兼容拒绝和停机恢复场景保持。

## Comments

- 2026-10-07: Claimed on `codex/interfaces-08` after dependencies 02–07 were resolved in integration.

## Answer

2026-10-07: 生产 Open 使用固定的构造、连接和启动路径。全部 checked 构造完成后连接真实循环，启动先执行 `complete()`，汇总八个 Core 模块及裁决、内容、执行、运行记录四个 Durable 域的 `ValidateDependencies`。顶层存储、正文、模块及工作实例先通过统一 RequireDependencies 检查；缺失项、nil 与 typed nil 返回具体模块和字段名，任何历史资格核验或恢复调用都在完成检查之后。`sqlite.Open` 的物理格式和身份核验保持原顺序，Open 的 65 秒期限和错误关闭存储契约保持。

获准的窄装配测试使用真实 SQLite、全部真实模块及各事务域 Work；计数包装器只在原 Store/Work/物理 IO 边界观察并委托原实现。缺 Trace 的真实 RED 曾在 panic 前执行三类历史资格查询并推进原 READY 目标。完成门禁后的 GREEN 返回 `missing required dependency: assembly.trace`，资格、恢复和物理 IO 边界次数均为零；通过公开 Durable 查询确认原回执不变、原 Job 仍为 READY 且 ClaimEpoch 为 0。14 个顶层对象及 15 个真实循环链接的 nil/typed nil 检查全部通过。公开兼容拒绝、生产执行及停机恢复场景另保留独立目标/供应商次数和原 owner 查询断言，不把内部 IO 边界计数冒充独立目标计数。

各必需端口及构造检查复用已完成的 02–07，完成检查不删支持路径必需能力。无调用旧构造已在各迁移票收缩，本票删除被完整连接覆盖的 `Tasks.WithCancellationJobs`，同步迁移 Sessions 和后续 14 的完整测试夹具；`WithCancellationClosures` 仍提供完整工作和关闭端口。装配只连接具体实现，资格与事实仍由原 owner 决定，不改变消息、schema、迁移摘要、格式身份、持久记录和支持版本。分层设计 11.1 在实现前更新；原 21 个资格/恢复阶段及固定调用身份与基线逐项相同，恢复 Module 的后续迁移留给 16/17。

候选 `51617d70a14d9da4158f7b95869e7a8b3e507732` 已同步集成 `0437fe9b94a8bfc99c2cdf2b303f4861368a68a6`，保留 12/14 的共享关闭交付。最终配置及公开 race 通过：assembly 8.113s、conformance 2.439s、durable 2.404s、sessions 2.780s、admission 19.084s；覆盖原目标恢复、完整生产 driver、模型重启不重复发送/计费、停机副本 UNKNOWN/原发送/取消/关闭后迟到账单和三类公开关闭交付验收。物理格式/外来 sidecar/不支持格式故障切片通过（conformance 1.657s、fault 4.169s），保存的 simulator/model 声明与绑定资格切片通过（3.689s），原 driver 实现/未知配置资格切片通过（3.012s）。`make check-code CHECK_PACKAGES='./cmd/assembly ./core/tasks ./core/durable ./conformance/sessions'` 通过格式、普通/fault 双 lint、规则与 scoped race；文档和差异检查通过。完整矩阵仍留给终点评审后的最终候选。

主集成合并无冲突。追加本 Answer 前的源码树为 `ca9f97051200c2e7381ed97e1105038b3c32e0ca`，与上述已测候选完全相同，因此复用同源精确记录。基线、真实 red/green、命令与版本记录见 `/tmp/lerna-module-interfaces-implementation/ticket-08.md`；日志为同目录 `08-merged-public-race.log`、`08-merged-check-code.log`、`08-merged-format-fault.log`、`08-merged-bindings-fault.log`、`08-merged-driver-history-fault.log`。本次只更新 08 状态、验收项和 Answer；父 spec 与三个用户原编辑不变，未混合后续 15/16/17 实现。
