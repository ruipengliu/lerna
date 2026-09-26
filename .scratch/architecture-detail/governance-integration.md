# 治理资产合并说明

1. governance-protocol-patch.json 的 defs 合入 protocol.schema.json/$defs，methods 合入 methods.json/methods。24项方法包括23个原reserved以及extensions.read投影扩展。Command/Query.method枚举按合并后完整registry统一生成，不保留reserved。
2. governance-mutations.json 追加到公共invalid-mutations.json。30–39例子已位于公共例子目录。
3. migrate_governance_fixtures.py 只产生迁移副本。当前只有原13-activation-online.json需要新增startup_evidence和instance_readiness；把governance-migrated-fixtures中的副本覆盖同名旧例子。14/15无active投影，不需改动。
4. 原exchanges.py中完整的 `if name=='extensions.read':` 块删除，由governance_rules.check_exchange处理install_lock/activation两投影及完整启动依据。检查其他领域结束前追加治理check_exchange，但应在共同Schema有效后调用；函数也跳过本域输入/输出Schema错误。
5. 原traces.py中 `if name=='extensions.read' and output['phase']=='active':` 整块删除。governance_rules.check_trace_rules完整接管原activation_intent以及新的历史依据、当前实例、reopen、时间窗口、原use关联。不要只保留旧instance==原use的判断，否则合法重启必失败。
6. 原审批fixture检查中target_id/lock_id保持精确；instance改为 `p['instance_id'] in approval.get('eligible_instance_ids', [approval['instance_id']])`。eligible_instance_ids只表达构造序列已核验的实例资格，不属于客户端认证请求字段。实际资格仍来自认证端点记录。
7. 原审批当前状态检查中，已知原use的历史回执重放不可改写为rejected：先确定 `replayed_use=name.endswith('check') and p['use_id'] in approvals`；仅新的use在invalid条件下必须拒绝。既有approval_use_immutable和窗口不续期检查保留。历史回执恢复不准许新的实际启动，治理的activation_current/approval_current负责检查已知撤回。
8. check_trace的结束处调用check_trace_rules(trace)，但Scenario或任何exchange的Schema错误存在时可跳过，避免对故意缺字段反例执行关联逻辑；函数内部也会跳过输入输出不合法的event。
9. 旧13用例中new_lock_id冲突仍由activation_approval命中，ready_instance=null仍由activation_readiness命中；原ApprovalUse不可续期的反例保留原共享检查。

独立验证：`python3 -B .scratch/architecture-detail/check_governance.py` 在内存合并本域资产并局部替换旧检查，不写共享Schema/registry。已通过10组本域序列及迁移后的原13序列和58个有目标规则的反例。不得把这些静态结果写成身份、事务、隔离或软件实际运行证据。

## 第二轮补齐：在线使用结算与业务 owner 确认

- 新增五个方法：grant.use.settle、grant.use.settlement、confirmation.request、confirmation.read、confirmation.decide。补丁现在包含 97 个定义、29 个方法规格；其中新增接口不是原 reserved 数量的限制。
- AuthContext 不在治理 defs 补丁中。把 governance-authcontext-properties.json 的 actor_kind、trusted_user_session_ref 合到最终 AuthContext.properties，保留运行组 sender_service_id。三者均是认证适配器给出的测试上下文，不是请求载荷可自行声明的身份。
- Scenario 仅新增可选 confirmations 数组，用于旧序列的已批准权威前提。每条记录绑定实际业务 owner、完整原 consumer Command、JCS 意图摘要及批准修订；实际 request/read/decide 流在 52–54 中独立覆盖。
- UseRequest 必填 usage_owner_id。governance-v2-migration-files.json 列出 14 个需要更新的旧文件；governance-v2-migrated-fixtures 内是对应完整副本。check/use 只增加计量 owner 及 grant.use 的认证发送方上下文，UseReceipt 保持原字段和不可变事实。
- 迁移中原 consumer 的 confirmation_ref 改为实际业务 owner、原 ID、批准修订 2。GrantIssue.intent_hash 使用规范意图重新计算，原业务输出及相应权威前提一起更新；Task acceptance 的 InputRequestFixture 也同步引用。
- 新 50–54 例子已经写到公共目录。governance-mutations.json 现为 85 个唯一名反例；第二轮 27 项均以文件编号作为名称前缀。不要另行追加旧 58 条产生重复。
- 原两个治理挂钩无需新增入口：check_exchange/check_trace_rules 已包括结算与确认。对 task.accept_result 的 owner／原命令／消费校验也在治理规则内，主校验器保持原候选、任务、目标及输入消费规则。
- 当前批准快照不是任意的 confirmation_ref 兜底。成功的 grant.issue、grant.lease.allocate、endpoint.pair.approve、evaluation.approve、task.accept_result 必须有先前确认流或 Scenario.confirmations 的精确批准记录。失败回执不消费。
- 规范意图通过现有 validate_transport.digest 复用 JCS；只在 grant.issue 中移除自身派生的 payload.intent_hash 后计算，避免自引用。不能改成 Python 普通 JSON 排序来模拟 JCS。

第二轮局部回归：22 组本域／迁移序列，85 个定向反例全部通过；全目录静态文档检查 33 篇、479 个本地链接、46 个 Mermaid 块，无错误。未新增实际服务或事务运行证据。
