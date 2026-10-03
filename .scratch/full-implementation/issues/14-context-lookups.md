# 14 context-lookups

Status: claimed
Blocked by: 01, 02, 03, 05, 08
Implementer: task_impl

依据：Brain 首版 Proposal need_context、Orchestrator 既有获准材料读取、C2 跨任务记忆。

补齐四种闭合 Lookup 的编码、原查询责任、有界读取及当前许可；结果持久保存为下一 Snapshot 的材料，来源不能变成本人原文证明。临时依赖等待原对象，禁止用重新请求模型探测；验证保存、纠正和撤回偏好对下一任务材料及结果的实际影响。

## 完成依据

实现、公开行为、真实负责方和必要故障正反例通过后记录准确提交与制品。外部资格单列；尚未完成的本地路径不得归因于缺凭据。未达成的目标保持 partial。

## Comments

2026-10-03：全项目范围复核后补入原实施任务图，未改变用户授权或领域裁决。

2026-10-03，第一垂直片（仍 claimed）：Brain/provider 已接闭合四 kind / 1..3 lookup 草稿，本方准确 publication query_local_id 转成原 QueryRef；真实 HTTP 模型一次请求及 SQLite 重开保留原 CallID/查询字节。Task 保存原查询 batch/Job、固定目标/控制、期限与累计 calls/bytes/tokens 保守上界；依赖只重试原查询，等待禁止新 Decision，已读材料保留而本人 SourceEvidence 不变。当前只有这些责任与 typed resolver/ContextCommitter seam 通过，开发宿主四 resolver 和实际 C2 偏好产物尚未完成，不能据此声称完整 need_context。选定 Brain/Task 正反例 race 实际 exit 0：4.323s / 46.465s；`need_context` 丢原材料 / 未接原查询责任 RED 分别 1.236s / 2.095s。
