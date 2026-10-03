# 18 skill-agent-configuration

Status: claimed
Blocked by: 01, 05, 06, 08
Implementer: governance_impl

依据：C6、Extensions 的 Skill／AgentConfig 精确记录，以及 Brain 第 5 节的渐进发现。

实现准确版本、正文、来源、前提、反例、依赖、冲突和退出规则的 Skill 登记及有界加载；AgentConfig 固定 Brain、能力和控制上限。当前权限先于读取，普通内容不能成为受信系统指令，配置不能扩大 Grant；注册、加载、撤回、重开和 Task Snapshot 的实际使用均须验证。

## 完成依据

实现、公开行为、真实负责方和必要故障正反例通过后记录准确提交与制品。外部资格单列；尚未完成的本地路径不得归因于缺凭据。未达成的目标保持 partial。

## Comments

2026-10-03 catalog宿主首片：准确Knowledge Content纯Tx门禁与普通材料用途接入Development；公开Skill注册→实际原validation Job→加载准确body/usage可运行。固定Gov公开tracer通过Go overlay在当前App真实执行：旧版RED5.873s current_knowledge_gate_unavailable，门禁接线后真实SQLite/PG GREEN14.426s。未启用Task知识选择、未改变默认File提案、未授Grant；Config/Context/Brain当前选择及物理控制边界仍待后续装配。旧publication nil policy只沿准确原reserve恢复，不随新增用途换policy。

2026-10-03：全项目范围复核后补入原实施任务图，未改变用户授权或领域裁决。
