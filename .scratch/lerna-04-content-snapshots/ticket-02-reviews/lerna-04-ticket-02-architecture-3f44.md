# 04票02：3f44最终固定源码架构followup

2026-10-04。BASE `f05b2f1068958ba6b63e6c1dc58d6cd1684446cc`，SOURCE `3f44032ffaaf9cd51fc253399412252767f0f737`，读取位置 `/tmp/lerna-worktrees/content-snapshots-02` 的固定git objects。实际8d64→3f44为15 paths、1283增/15删；base→source为7 commits/49 paths。未读取Std/Spec或新轴finding，未运行native/Go/build/test/DB、未访问环境/凭据、未清理或修改仓库。

**结论：自身8d64报告的F1已源码闭合；当前无新增Strong必要重构。保持具体typed readers、私有自然/历史规则和现有owner装配。** 这是架构源码资格，不是7AC resolved、whole04退出或最终normal/race/check/audit/CI通过。

## 1. 范围承接与完整变更读取

以自身 `/tmp/lerna-04-ticket-02-architecture-8d64.md` 及此前57ea读取为基线；本次逐项固定差量读取，并对全tree比较mode/type/blob：895个保留条目完全相同，15个差异均为100644 blob（8个修改、7个新增），没有隐藏mode/type改变。旧已读implementation承接准确相同对象，不把另一个reviewer的审查作为自身读取。

15项覆盖：`.gitattributes`；domain/content/management.go；PG content management及management_rows_test；公开content_legacy_test、content_processing_deadline_test；contentfixture/legacy.go及testdata/README；新legacy-expired-ancestor目录全部7项（2份对象原字节、SHA256SUMS、observation.json、original-pg18-dump.sql、producer.go.txt、restore.sql）。

源码/测试/producer及文档完整读取；SQL两份整个字节流读取、DDL检查、全部COPY数据解码、全部79行与恢复INSERT及剩余非session文本做机械等价核对，65个guest记录逐项规范化比较，未抽样代替全量。没有执行SQL；这项静态archive核对不冒实际升级成功或完整运行审计。

关键最终blob：

| 对象 | 3f44 blob |
| --- | --- |
| domain/content/management.go | bea1dc9703538ff7f825d819775eb5ca8ac212a6 |
| adapters/postgres/content/management.go | 185165840a9bde50f0234e99150a6a50fb00c40c |
| management_rows_test.go | 0d3b5d44f3f0d6ad495a41a103d4a7de1629cd92 |
| content_processing_deadline_test.go | da5e7618fd0c4c46cbe7847a09b3b526f7c12edd |
| content_legacy_test.go | 2f2089c418055e781a3710d1dd9d89da9a7d58a6 |
| contentfixture/legacy.go | 06438b6fda8cff8f38a657fafbc83fe44e7f7cae |

## 2. F1 closure：原自然阶段交接

**准确位置：** `domain/content/management.go:956–973`。原 `now.Before(change.ExpiryDue)` 已改为 `change.Phase == "policy_change"`。完整初始页序完成时，同key、原watermark、cursor重置，切natural_expiry并取原ExpiryDue/ExpiryDeadline；已越过该自然预算则residual/original_deadline_expired。自然阶段自身完成不再重排自己。

原visited保存的初始phase/Deadline/游标及最终所有change资格保持，后续等待跨原初始Deadline仍恢复原初始残留，不能靠切较晚自然截止绕过去。未改原Policy/Previous或receipt/版本身份；PG已保留历史pending及原较早责任截止，不让自然not_required清掉撤销历史。原current/fullRef/主体用途/cap与coverage核对路径未变。

新增 `TestContentDelayedHistoricalPageHandsOffOriginalNaturalValidUntil`（processing_deadline_test.go:242）给出早/晚正常对照：只收紧ValidUntil、retention仍长，真实公开安装/推进/观察；晚路径检查后代pending、五动作及原natural阶段/时刻/水位。它明确覆盖旧RetainUntil测试不能证明的分支。该新增测试使用Manager入口，源码共享推进仍同时被Content.Step消费；不把新增单项叫“两入口独立native证据”。此前两入口期限测试仍在原对象范围中。

**架构判断：Strong已源码关闭。** deep module的interface没有增加；private阶段交接集中同一owner的历史与当前资格，两个实际推进入口获得同一修正的leverage。deletion test：删共享推进会复制phase/原预算/游标规则，降低locality。无需registry、通用状态机或新ADR。

Before（8d64）：历史原Due→now已到expiry→直接complete漏自然维护。
After（3f44）：初始原最终资格→原自然阶段→当前核对/原截止残留；自然完成不自循环。

## 3. KEEP：六类实际typed rows消费

**Files：** adapters/postgres/content/management.go、management_rows_test.go。本次将UnindexedVersions、PendingChanges的真实rows消费提到各自私有typed函数，沿既有四类reader保留primary、Rows.Err和Rows.Close。SQL、row类型和cursor语义仍各自明确，未引入泛型callback平台。

新增机械driver输入columns与normal/EOF Close/decode+Close/scan+Close/iteration情形；同一private消费函数是实际查询调用者和机械测试的interface。原生产PG adapter唯一，不把driver seam称第二业务adapter或真实PG nativeClose故障。正常source是否实际通过最终完整检查由sole owner另提供。

**强度：Worth exploring / KEEP。** 六种形状不自动构成提取框架理由；删除typed helpers会把同一消费知识塞回查询函数并削弱机械cause验证的locality，没有减少业务复杂度。提泛page框架仍需六类解码/游标知识，当前无额外leverage，不设新门槛。

## 4. KEEP：真实旧截止归档与独立恢复观察

新增归档声明生产来源为准确 `1a7d910238eb74cddc712d92b0ba4014a72ff507` 的已停止writer；producer源码是证据输入，未参与当前产品构建。它创建三原Command/版本链，source ValidUntil=原运行时固定近截止，retention与receipt保持2099；先产生两个published及一个failed staging，再安装65guest。producer有限20秒且聚合成功Open后的Store/Object/文件Sync/Close错误。此正常producer源码不证明所有partialOpen/物理Close失败路径已经执行或得到ACK。

本次静态核到：原native dump **112782 bytes**，SHA256 `f0be7063ce24ab1f491e316ec4028df82e5f630e545a9f443d708b2d96d8d829`；清单全部6个digest匹配。2个原对象均准确 `alpha\n`、6 bytes，hash b6a98d…51060。79行分布=3 Command、1 reader、68 policies、1 migration、3版本、3 Job。恢复文本完整机械归一一致：只改namespace、COPY→等值INSERT及去session/psql/CREATE SCHEMA行，原bytea/值未变；migration仍仅0001 checksum `00363b79dafb6eb1f373be9ef08e915fe23cc3db0fa55345e8ae6c051ce0c1ed`。

原观察SourceValidUntil=`2026-10-04T16:00:30.057895Z`；source/middle真实记录admitted_at均早于该值，io_deadline保留原值；所有原CurrentRetainUntil仍2099，故这是许可自然过期，不是伪造保留cap到期。failed保留staging `alpha\n`、原failed/forbidden与原Command；三个Job皆原publish/done，没有当前0002的传播事实倒灌入旧archive。

`contentfixture/legacy.go`只增加第三确定archive入口/嵌入/观察字段，沿原独立受登记namespace、有限restore Tx、sticky数据库Close、setup file/dir Sync/Close和原迁移检查。新 `.gitattributes`只保该原native SQL字节，不改旧档。

新增 `TestContentActualExpiredOldWriterArchiveRetainsAncestorResponsibilities`：有限40页/每页2、第一页reopen，原3receipt/publication不变、旧读取拒绝、原祖先Due+一分钟原预算残留、published object/failed staging各自pending，原2份独立字节仍在；另从本来获准的独立输入建立新source/derived并公开读回作正常对照，未从过期旧源派生或刷新原ref。最后reopen仍核原截止。

**强度：Worth exploring / KEEP。** 第三archive是新增真实场景，不是新storage adapter；复用小fixture入口隐藏恢复机械知识，有现成locality/leverage。deletion test：复制restore/owned close进测试只会分散责任。维持这个seam，不造通用备份恢复平台。

这里核定的是归档文件、producer与测试观察的源码/静态一致性。README所述native exit0/group absence、原scope直接升级及保留路径仍属于执行证据轴；本代理未运行producer/dump/restore、未核所有外层原日志，不据文字声明替其背书。

## 5. 最终资格及建议

维持improve-codebase-architecture / codebase-design原建议：完整闭包、自然/历史分类与当前coverage、已知holder登记和原期限资格留在现Content管理module；具体PG adapter落实事实锁/持久单调合并，Host不承接领域裁决。与根CONTEXT及ADR0004/0006/0007无新冲突，不新增ADR、领域interface或未来依赖。

当前**0项新增Strong必要架构修正**。source F1 closure不抹旧失败，也不替代最终normal/race/check/audit/CI；7AC未resolved、whole04未退出。05条件决定尚未采用，继续硬等whole04，不成为当前票02门槛。此固定source之后若仅fixture/docs变更，仍按准确changed objects核定，不把未读新pin提前纳入结论。

HTML与实际打开结果附下；CDN是增强，inline CSS/静态方框和完整文字可离线阅读。

HTML：`/tmp/architecture-review-20261004T161543Z-content02-3f44.html`。实际 `xdg-open` exit3，无可用打开方法/文本浏览器；未浏览、未验证CDN渲染。保留inline CSS、完整静态图文和Mermaid原文fallback；没有下载依赖或安装浏览器。
