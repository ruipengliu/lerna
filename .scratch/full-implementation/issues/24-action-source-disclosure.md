# 24 action-source-disclosure

Status: partial
Blocked by: 01, 02
Implementer: task_impl

依据：Brain 原参数 publication 的显式来源声明，以及 Search／Body 真正出站时的 processed／disclosed 双门禁。

已确认 materialize(act) 把 ActionCandidate.disclosed_source_refs 永久置空，丢弃原 ArgumentsLocalID 对应 publication 已保存的明确 DisclosedSources。机械保留原声明；不猜测接收方、不将全部 processed 自动公开、不让宿主补造模型未声明的披露。原数据、身份、版本及未结出版责任仍可恢复。

## 完成依据

真实 HTTP 草稿 → Brain 公共 Proposal 证明精确披露保留、没有声明时仍为空、原出版重开不换内容；Search 宿主闭环证明合法查询出站与缺声明拒绝。确认修复由同一最后 review implementer 完成。

## Comments

2026-10-03：Search Task 装配实际阻塞后核对固定源码，原声明存在于 pendingContent，ActionCandidate 构造处将其丢弃。

## 本方已验证 producer

Brain 机械保留原参数 publication 的明确来源声明；新增闭合可选 `DraftAction.disclosed_local_ids`（最多20、已存在且不重复），映射原已出版引用，缺字段不增加披露。Provider Schema/解析与 Brain 同版；未声明、未知、重复与超界拒绝，不自动公开全部 processed。合法 complete 的必填空 check_suggestions 同时修复，Task 空条件完成门禁保持原值。

真实 HTTP → 原 Brain Proposal → SQLite/文件出版重开 → 原命令回执重放正反例 race PASS 8.081s；准确原 source 丢失先 RED 2.294s，新 local 字段旧 Schema RED 0.026s，必填空数组先 RED 0.333s。原 source、明确参数自身、二者并存及无声明四种行为通过，未知/重复/超过20拒绝且恰好20仍合法。初版测试快照漏登记 capability/binding 被供应商门禁拒绝是夹具诊断，未当作修复 RED。

尚待工单15消费侧实际 Task Search/Body 闭环与完整新集成验收；当前本方 producer 通过不把该端到端范围记为resolved。
