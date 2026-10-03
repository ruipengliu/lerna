# 24 action-source-disclosure

Status: claimed
Blocked by: 01, 02
Implementer: task_impl

依据：Brain 原参数 publication 的显式来源声明，以及 Search／Body 真正出站时的 processed／disclosed 双门禁。

已确认 materialize(act) 把 ActionCandidate.disclosed_source_refs 永久置空，丢弃原 ArgumentsLocalID 对应 publication 已保存的明确 DisclosedSources。机械保留原声明；不猜测接收方、不将全部 processed 自动公开、不让宿主补造模型未声明的披露。原数据、身份、版本及未结出版责任仍可恢复。

## 完成依据

真实 HTTP 草稿 → Brain 公共 Proposal 证明精确披露保留、没有声明时仍为空、原出版重开不换内容；Search 宿主闭环证明合法查询出站与缺声明拒绝。确认修复由同一最后 review implementer 完成。

## Comments

2026-10-03：Search Task 装配实际阻塞后核对固定源码，原声明存在于 pendingContent，ActionCandidate 构造处将其丢弃。
