# 04票02：752单测试差量架构资格

2026-10-04。BASE `f05b2f1068958ba6b63e6c1dc58d6cd1684446cc`；prior `3f44032ffaaf9cd51fc253399412252767f0f737`；本次固定 `75202ee0077b5bf12b432b5017ad97a1dfcab681`。只读git objects，未运行native/Go/build/test/DB、未读取环境或对轴报告，未修改仓库。

**结论：0项新增必要架构修正，F1源码闭合资格承接3f44。** 全tree逐mode/type/blob核对，仅 `conformance/component/content_processing_deadline_test.go` 改变：100644 blob `da5e7618fd0c4c46cbe7847a09b3b526f7c12edd` → `5cf994ae4aec2ef7ed91f87a77453f2ea345c2cc`；其余909个条目完全相同。base→752实际8 commits/49 paths。全部产品、SQL、archive、fixture等对象准确承接自身已读3f44及更早范围，没有借用另一审查轴资格。

已全文读取该固定测试对象。`:242` 的 `TestContentDelayedHistoricalPageHandsOffOriginalNaturalValidUntil` 现在由 `consumer={manager,service}` × `late={false,true}` 形成四个实际场景，各有独立fixture、15秒有限context和原near ValidUntil，分别真实调用Manager.Step或Content.Step。它们共用同一公开管理观察：正常not_required并读回正文；迟到须pending、五动作、ObjectHolder及原natural phase/Due/Deadline/Watermark。仍只收紧ValidUntil，retention cap保持长，故没有借cap失效遮住F1。原文件其余期限、最终多change和retention场景未改变。

这补齐了3f44报告“新增单项仅Manager”的源码覆盖限制；不把两个入口叫两个storage adapter。测试跨原module interface验证同一private阶段交接，保持locality/leverage，不扩大interface、引入新框架或重开ADR。原自然/历史分类、原预算最终资格、typed readers、历史pending及旧writer恢复结构均不变。

执行owner报告focused 2.667s与Local full四tests .065s；本代理未执行/未独立核原日志。race仍在compile，不能据此声称race通过、最终全套green或清理责任已确认。7AC未resolved、whole04未退出，原unknown与最终normal/race/check/audit/CI资格继续独立。

沿用架构说明HTML `/tmp/architecture-review-20261004T161543Z-content02-3f44.html`，其标题/source pin仍为3f44历史报告；本补记承接到752，不倒改原事实。该HTML实际xdg-open exit3、headless未浏览/CDN未验证，inline CSS及静态图文fallback可读。纯测试delta无需新HTML或重新打开。
