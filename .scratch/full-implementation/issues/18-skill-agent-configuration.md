# 18 skill-agent-configuration

Status: claimed
Blocked by: 01, 05, 06, 08
Implementer: governance_impl

依据：C6、Extensions 的 Skill／AgentConfig 精确记录，以及 Brain 第 5 节的渐进发现。

实现准确版本、正文、来源、前提、反例、依赖、冲突和退出规则的 Skill 登记及有界加载；AgentConfig 固定 Brain、能力和控制上限。当前权限先于读取，普通内容不能成为受信系统指令，配置不能扩大 Grant；注册、加载、撤回、重开和 Task Snapshot 的实际使用均须验证。

## 完成依据

实现、公开行为、真实负责方和必要故障正反例通过后记录准确提交与制品。外部资格单列；尚未完成的本地路径不得归因于缺凭据。未达成的目标保持 partial。

## Comments

2026-10-03：全项目范围复核后补入原实施任务图，未改变用户授权或领域裁决。
