# 24 action-source-disclosure

Status: resolved
Implementer: task_impl

范围为Brain原行动参数publication的显式披露声明、严格Provider草稿合同与真实Search/Body消费，不能自动公开processed来源。

## 完成依据

producer leaf`32e3d79`机械合并原Args publication.DisclosedSources与闭合可选DraftAction.disclosed_local_ids≤20的明确已出版LocalID；未知/重复/超界拒绝，无字段保持空。Provider/Brain Schema与解析同版；恰20合法，不能默认披露全部source，也不要求内容自引用。

真实HTTP→Brain公共Proposal→SQLite/文件出版重开→原command receipt正反例race8.081s实际通过；原source丢失RED2.294s、新local旧Schema RED0.026s、complete必填空check_suggestions RED0.333s保留。4种source/参数自身/并存/无声明与20边界均实际验证，Task空条件完成门禁未放宽。

消费已由工单15完成，不再标待接线：`577bb23`的真实Task/Gov/Content/Source两库矩阵race880.295s验证合法Search query_ref在processed/disclosed双集合、Body参数由模型显式[args]声明、缺声明零HTTP；后继`action-information-reference-answer-verification.json`真实两库626.122s由原问题经准确Source与独立引用核验发布verified Result。每份source/测试摘要与失败独立保留，真实自然语言质量/live账户不由该修复宣称。
