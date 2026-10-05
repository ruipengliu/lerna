# 04票05：恢复save后的清理责任资格更新

**采用独立、受信、精确比较后更新的Lifecycle写入seam；普通SaveResponsibility不放宽。** 当前首tracer只实现“原暂时save撤销、现在保存依据完整恢复且未seal”的not_required分支。未知、expiredcap、sealed等保持原pending，不在这一条tracer中顺便自动Seal或声明整票完成。

固定源码：`3aa948db98e1ab309a9479f74a8c1090bc59838d`，WT `/tmp/lerna-worktrees/content-snapshots-05`。读取management/lifecycle/closure/service与PG management/lifecycle；新未跟踪 `domain/content/policy_cleanup.go`当前仅ErrUnavailable stub，`conformance/component/content_policy_cleanup_test.go`是待运行静态tracer。本次没有native/DB执行，不把该测试称为已red或green。

## 1. 原规则与最小新增端口

`domain/content/management.go:register`把原change、完整保存主体/用途、五动作、原deadline和实际holder/attempt/publication观察投影为CleanupResponsibility。PG `adapters/postgres/content/management.go:324` 的SaveResponsibility特意保留previous pending/residual、holder正向事实、原attempt、较早deadline及action union，防止普通renewal洗掉历史责任。这条规则保留原样。

在已有 `LifecycleRepository`增加单一窄写入端口，例如 `QualifyPolicyCleanupNotRequired(ctx, tx, expected CleanupResponsibility) error`。这是Lifecycle的实际消费接口，不放进公共Content合同，不给Manager普通写路径一个任意“允许降级”flag。PG实现锁**已有**准确责任行，比较expected，再只更新BodyCleanup为not_required及Residual为明确 `current_save_basis_restored`。不得UPSERT缺失行，不接受任意新状态或新deadline。

expected须绑定原ChangeKey、完整Ref、完整Subject/DelegationChain、Purpose、Actions、Deadline、Reason、holder/attempt/publication及原结论等全部已观察字段。最简单是解码后完整值比较/实际CAS，不能仅凭同object_id；不一致返回已有scope/conflict错误，回滚本页，不覆盖并发新增的责任。若已是同一准确qualified结论，可在重新当前资格后幂等；不借幂等跳过current authority。

原Change的policy/revision/watermark/phase/history、责任Actions/Reason/Deadline、StagingHolder/ObjectHolder/AttemptKey/Publication不改。not_required只表示**这条历史触发现在不需要启动物理删除**，不是erased、不证明所有holder无字节。Record.CleanupPending不粗清，其他change、holder、残留或未决attempt不因此消失。现有表body可表达这两个字段变化，无需迁移或新ADT。

## 2. 必须具备的当前资格

1. 消费入口沿Lifecycle现有精确owner/TrustedSubject/TrustedUntil、有限ctx、配置PageSize≤64及cursor/key长度检查。责任完整主体/用途须等于目标D的**原Record.Subject/Purpose**；当前管理主体具有该原保存范围，不能仅比SubjectID，不能以别的主体或用途的许可取消D清理。
2. 当前首分支仅接原已确认的临时save撤销责任：原准确来源绑定/历史basis可核、当前pending的原因属于这次保存撤销，非维护coverage_unknown/未知旧phase/错fullRef/额外未确认因果的泛化清理。历史并发增加的原因/actions必须重新核对，不能不分原因覆盖。
3. 在同Content owner更新Tx中，核D准确版本、身份、当前单调CurrentRetainUntil未过期、BodySeal=nil且BodyGone=false。target当前save及**全部已登记祖先**在D原完整主体/用途下当前save都须获准；完整ref、policy ValidUntil/RetainUntil、祖先准确published/单调cap/unsealed、完整≤64闭包仍执行。这里不是新处理或披露，无须自动额外要求read/process/disclose；其他动作历史仍保留。首次tracer仅published目标正常，不能顺手把failed/staging所有情形都宣称已覆盖。
4. 当前source宽policy不能提升任何旧有效/当前cap，也不能解除seal。旧责任属于另一个完整主体/用途，只保留原use_review/not_required，不赋予删除或取消原保存者责任的权限。
5. 取得必要锁之后，以及责任写入/实际等待之后，重新采DB当前时间检查管理授权和全部本次观察的最早policy/cap期限。任何到期/存储错误回滚本页qualified写；授权失效不能作为一个正常pending观察对外泄露事实。已锁事实不跨Tx缓存。

## 3. 锁顺序与有限分页的具体默认

现Responsibilities查询不锁行；ReadChange则是 **FOR UPDATE**。Manager推进会先锁source/target Version，最后SaveResponsibility锁责任行。新消费者不得“锁责任或ReadChange → 再锁Version”，亦不得把Manager.ObserveChange的持锁事务与新版本资格事务拼成一个大Tx。

最小无新增读取port接法：先用现有受信 `Manager.ObserveChange` 在独立短Tx取得原change及有限候选页并完全释放；然后另一个有限owner Tx完成当前资格和精确更新。旧候选只作选择身份，不作授权证明。更新Tx不先调用ReadChange，也不修改change/传播cursor；原不可变关联由expected责任比较和准确原change身份核对固定。

**同页两阶段：** 先按确定顺序完成本页所有待qualified目标/祖先的实际policy与Version资格，收集各自有效界限；其后才逐项进入新端口的责任行锁/CAS。不能处理第一行时先写/锁责任，随后又去取得下一行Version锁。首目标policy→Version及闭包沿现有当前资格机制，不用新增反向的排他policy升级。任何SQL死锁/lock timeout仍为真实失败，不能宣称此局部顺序证明全库无死锁。

新端口取得责任行后不再反向锁Version/change；本页所有更新后最后fresh clock检查，失败全部rollback。返回当前PropagationObservation可在成功提交后沿原ObserveChange重新读取；若该最终查询失败，不编造成功观察，也不撤销已提交事实，调用方在原change/key/cursor重试核对即可。查询响应不承诺与此前候选页跨Tx原子。

NextCursor仍是实际责任object稳定排序分页，不能把首个空/不适用页当全集完成；一次调用只消费有界页，无后台新Job。并发新增较小cursor的责任不由这一页自动证明已处理，全change完成需要原传播状态/完整遍历另行核对；本入口不写“全清理完成”。普通后续传播若保守重新记pending，可再按当前条件核对，不能把一次not_required当未来豁免凭据。

## 4. 原deadline的准确处理

责任Deadline及原change Due/Deadline/ExpiryDue/ExpiryDeadline一律原样保留，不因续期或本次调用设now+budget。D/祖先当前cap已过期绝不qualified，不以新宽policy复活。

**原维护执行deadline与当前核对权限分开：** 历史责任deadline已过去，不自动禁止在新的有限、当前受信查询/核对调用中确认“现在无需启动删除”；此更新不执行物理效果、不继续原过期Job、不提升余额/lease，也不把原change residual改成complete。允许这一准确核对，原过期时间及历史延误仍原样可见。若该责任另有coverage未知、已seal、在途清理或其他未确认原因，当前首分支仍保持pending；不能用“仅核对”借口擦掉它们。

未来确需Seal的分支必须单独沿原负责范围/有限当前管理授权处理；本首tracer不刷新原执行期限或暗启动新删除工作。最终管理可信时限、caller deadline、当前policy/cap始终是本次资格的实际门。

## 5. 首tracer与必要拒绝的资格

现静态tracer的方向正确：真实出版→savefalse并实际传播→见原pending及真实正文→save恢复→重开→Consume→原change/receipt/holder历史不变且not_required→无seal/无body_cleanup工作→独立原字节仍在→重开观察仍保留结论。不能以此一个正常案例证明全集管理/cleanup通过。

随后同范围需要无权/错完整主体、仍savefalse、祖先savefalse、当前cap过期、已seal、责任CAS变化、真实等待跨当前期限等拒绝/回滚正常对照。观察走原Manager/Lifecycle/Content/Command端口与独立对象，不读私表作为业务oracle；fixture机械阻塞须与真实PG等待分别标明。首单尚未native，以上均为需要实现验证的边界。

## 6. 独立相邻必要小修：ObjectHolder与权威gone

读取冻结01 `service.go:735/738` 与02 `management.go:425` 可见：ObjectHolder在实际对象I/O后更新，并被投影为该版本primary的holder责任；Publication/receipt另保历史。它不是不可变“曾有过正文”字段。但其false也一直不是物理不存在证明，部分I/O失败仍可能留字节/attempt。不要凭该bool删除未知责任。

当前3aa `lifecycle.go:490` 在staging+primary真实erased的权威ACK之后只置BodyGone，不清ObjectHolder；`service.go:959`迟到失败又可据旧成功ioErr把ObjectHolder设true。采用独立最小修正：权威准确ACK同Tx置 `BodyGone=true`、`ObjectHolder=false`，条件覆盖“BodyGone已true但旧ObjectHolder仍true”；迟到publication失败赋值改为 `ioErr == nil && !record.BodyGone`。保留其现有最终seal/claim/版本门禁，不以此bool修正替代文件fence。

不清历史CleanupResponsibility的holder union/原attempt，不改secondary责任、原Command或published历史，不把未知erase当ACK。ObjectHolder=false只反映该primary已确认不再持有的当前事实；ALLholders完成仍由真实holder集合独立决定。

验证与首restored-save red分开：真实权威正文擦除及受信Lifecycle holder观察、当前获准metadata gone、原Command历史；其后**新准确policy change**的公开受信Observe责任应反映当前primary ObjectHolder=false，旧change历史仍保留原true。另在实际Put成功后/最终保存前有限门，让真实Seal+权威ACK先完成，再放行旧Finish，核后续新责任不重造primary持有事实；不通过私表镜像assert或假对象回执制造证据。当前只作源码语义判定，相关真实正常/晚回调suite完成前不称已验证。

本决定不扩大为整票架构重构，无新ADR、公共delete/Grant或通用清理框架；必要修正可由原solefixer分开tracer落实。
