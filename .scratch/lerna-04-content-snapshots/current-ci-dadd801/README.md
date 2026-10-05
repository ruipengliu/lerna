# 当前提交 CI 失败记录

准确提交 `dadd801cfd11ca038f0496872941097f10ebe507` 的 push 运行 [37274041977](https://github.com/ruipengliu/lerna/actions/runs/37274041977) 于 2026-10-05 06:47:25 UTC 结束，结论 failure。通过 GitHub 通用只读接口查询实际 push 集合；只查询 PR 的专用接口及 commit statuses 空结果不能证明没有 CI。

- contracts：`make check` 的格式检查扫描了归档审计程序及 Manager 测试的预格式副本。两份归档现已保字节改用 `.go.txt`，原执行程序、历史路径与 SHA 不重写。
- durable-admission：`TestPGPoolRunReservedLaneProgress` 在正常取消后返回 `driver: bad connection`，失败位置 `pool_test.go:931`，单例 0.14s，Recovery 包 25.727s。取消原因保存的必要修正仍在评估。
- Component 和竞态集成没有执行，不构成当前源码资格，也不能解释大图容量。

[观察元数据](observed.json) 固定提交、run/job、日志 SHA 与读取范围。[基础日志](contracts.log) 和 [集成日志](durable.log) 在写入前已脱敏；SHA 对应脱敏副本。root 完整机器处理日志，人工读取相关失败片段，未声称全文阅读启动与容器日志。此前本地资格及该次远程失败分别保留。

[必要修正决定](adopted-cancellation-decision.md) 已完整读取并采用。[当前归档映射](archive-packaging-map.json) 记录原归档名、新名和不变 SHA；[原审计副本](../ticket-05-expired-job-scan/current-resource-audit/audit.go.txt) 与 [Manager 预格式副本](../ticket-05-expired-job-scan/manager-full64-qualification/source-preformat-content_manager_expired_backlog_test.go.txt) 不作为当前 Go 构建源。原 outside 审计路径 `/tmp/lerna-05-own-resource-audit-static/audit.go` 及其实际执行 manifest 保持；旧 root-normal-verification.json 的归档名是当时事实，当前查找使用此映射。
