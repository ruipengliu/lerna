# 19 grant-list-recovery

Status: resolved
Blocked by: 01, 02, 06
Implementer: execution_impl

依据：已开放 Grant 方法族必须具备 grant.read／list／check 恢复接口。

补齐当前主体／管理权限下的有界授权分页；数据变化、权限、租户、角色及原期限绑定游标，避免泄露无权 Grant 身份。真实公开接口覆盖合法本人／authority、跨主体拒绝、撤回／到期、超过一页及双库。

## 完成依据

实现、公开行为、真实负责方和必要故障正反例通过后记录准确提交与制品。外部资格单列；尚未完成的本地路径不得归因于缺凭据。未达成的目标保持 partial。

2026-10-03：新 grant.list 闭合 api.ListInput → api.Page[GrantRecord]、pureTx 当前凭据门禁、完整 Grant+Usage 摘要和安全 revision、100 耐久原查询槽/HMAC cursor 已实现。首次/后页绑定准确主体/roles/gen、tenant/owner/DB、全 view 和首次期限；超过 999 项/256KiB/100 槽显式 overloaded，不返回部分全集、不做 Body/Source IO、不增加全局 HeadLock。

固定验证源码 30478f5（实现 a07b9c1、设计合同 8d66fce，已合 code-dev 7c8b0b1）：SQLite/真实 PG17 全授权列表专项 race 包退出 0，182.721/189.910 秒；205 项分页、首/后页当前权限收缩、public issue/confirmed revoke/Use、expiry/notbefore、原 TTL、quota、边界和原 query/cursor 重开均通过。真实 App管理角色→HTTPS→认证发现→GoSDK 普通主体双库专项通过 39.14 秒；TS/Native generate:check、vet、diffcheck 通过。外部制品 /workspace/harness-dev-environment/grant-list-verification.json 保存准确 scope/query refs、原日志/退出码和 backend，不把配置 PG 环境误当 SQLite 案例的 backend。

范围仅原方法族的有界参考列表恢复；不声称生产 SSO、全包 race、浏览器新增专项或整项 hosted CI 已运行。App 未装当前门禁时仍明确 unsupported。

## Comments

2026-10-03：全项目范围复核后补入原实施任务图，未改变用户授权或领域裁决。

2026-10-03：Root 将新功能实现转交 execution_impl；Task 保留 14 和正式 review 修复。
