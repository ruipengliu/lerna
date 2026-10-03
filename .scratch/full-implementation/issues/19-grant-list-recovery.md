# 19 grant-list-recovery

Status: claimed
Blocked by: 01, 02, 06
Implementer: execution_impl

依据：已开放 Grant 方法族必须具备 grant.read／list／check 恢复接口。

补齐当前主体／管理权限下的有界授权分页；数据变化、权限、租户、角色及原期限绑定游标，避免泄露无权 Grant 身份。真实公开接口覆盖合法本人／authority、跨主体拒绝、撤回／到期、超过一页及双库。

## 完成依据

实现、公开行为、真实负责方和必要故障正反例通过后记录准确提交与制品。外部资格单列；尚未完成的本地路径不得归因于缺凭据。未达成的目标保持 partial。

## Comments

2026-10-03：全项目范围复核后补入原实施任务图，未改变用户授权或领域裁决。

2026-10-03：Root 将新功能实现转交 execution_impl；Task 保留 14 和正式 review 修复。
