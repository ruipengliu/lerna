# 04票05：原政策责任 → seal → 全holder ACK

**采用最小关联：原政策消费在同owner Tx建立seal/holders/Job并核对原pending责任；实际Lifecycle.Step仅在该seal全部holder独立确认后，把该原责任更新为erased。** 普通SaveResponsibility继续保守合并，不开放任意降级flag。

固定partial：`c2716c0486f2304827ce8073189587c58064a6a1`，WT `/tmp/lerna-worktrees/content-snapshots-05`。已读当前policy_cleanup/lifecycle/management、PG相应ports及上次 `/tmp/lerna-04-ticket-05-policy-cleanup-qualification-decision.md`。恢复save首分支0.366/race1.745 native0/absent是root核实的已有范围，不是本次执行。新 `TestContentCurrentSaveWithdrawalStartsOriginalSealAndRealCleanupConsumer`仅作为待验收tracer读取，不宣称其red/green已完成。

## 1. 当前缺口与具体最小接法

现ConsumePolicyCleanup只作not_required；BodySeal无政策来源key；Step在AllBodyHoldersErased后只Complete工作，不更新原政策责任。因此新tracer期待的原责任erased不能靠现有普通SaveResponsibility实现。

在内部BodySeal增加准确 `PolicyChangeKey`（可省略，旧/主动Seal为空）。它与完整Ref、原Subject/Purpose、SealID、原Deadline共同确定本次责任关联；不把policy key当全局seal身份。policy SealID从owner/准确版本/原change key的稳定有限编码或摘要确定，重试重开不新ID。旧JSON缺此字段明确是“没有政策关联”，不猜测补key；不改旧迁移/公共wire。

新增Lifecycle专用窄端口完成两件实际工作，可共享PG私有精确row读取/CAS机械部分：

- **seal绑定核对**：锁已有原责任并与候选expected全部字段比较，要求同原key/完整ref/原完整主体用途/deadline且pending；不UPSERT、不改变原历史。该锁/核对与Record内PolicyChangeKey、seal、所有holder、cleanup Job在同Tx提交，绑定时责任仍pending，不能提前写erased。
- **全ACK更新**：供实际Step使用，按已锁定的准确BodySeal定位该原责任，核相同key/ref/Subject/Purpose/Deadline及合法pending（同结论重放可幂等），只置BodyCleanup=erased、Residual=""；保留原Actions/Reason、历史holder union、AttemptKey、Publication及原Deadline。匹配失败/缺行是错误，不造一条新责任。读取到的原row与更新之间仍用精确CAS；不得覆盖并发变更。

没有必要给业务增加通用transition接口或把整条责任复制成新的领域账本。最终ACK port接受准确seal绑定，实际旧责任在Version锁之后被锁定，其当前历史字段原样保留；无需将历史holder布尔清零以“证明”擦除。

## 2. 真正能启动seal的政策因果

沿已有trusted lifecycle当前owner、完整管理subject、绝对TrustedUntil、有限ctx和原Record完整保存Subject/Purpose。页面候选只选身份，不提供当前删除许可；原change/source必须确实是D本身或D完整已登记祖先，准确ref/用途/完整delegation一致。其他主体/用途只影响自己的use_review，不能删除原保存者正文。

新首分支仅覆盖**published正常版本、原保存范围内准确save撤销、当前确仍savefalse**。必须读到当前确切policy及其语义；CheckPolicy返回nil还可能是缺policy、期限、绑定或其他失败，不能仅凭nil推断“准许删除”。在真实原来源关系下，当前target/适用source明确savefalse，或原不可提升CurrentRetainUntil已过期，才有对应保存依据失效的确定因果。缺来源、错fullRef、DB错误、coverage_unknown等按unknown/pending处理，不能把不可核验等同撤权证据。

只撤read/process/disclose不启动删除；也不能要求先重新获得save=true才能执行合法清理。判定必须保留“当前save完整恢复→not_required”和“保存依据确实失效→seal”两分支，其余保留pending。expired-cap及祖先因果虽是既定完整义务，须后续单独tracer验证，本首案例不冒覆盖。

## 3. 同Tx共享seal步骤与锁序

保留已采用的“候选Observe独立Tx并释放 → qualification Tx”的顺序。全页先完成必要policy/Version/完整来源关系锁与资格，不先锁ReadChange或责任行。随后在该Tx内按确定顺序建立必要seal/holder/Job，最后对全部候选原责任行核对/CAS；任何失配整页回滚，包括刚建立的seal。责任行锁后不再反向获取Version/policy/change锁。

**不要在Consume中调用会另开Tx的公开Seal，也不要复制一套seal实现。** 抽取现lifecycle.go真实封闭步骤为私有、消费已经锁定Record的函数，现有Seal、SealOrphan和新policy分支都调用它；各入口保留自己的授权/因果/孤儿检查，公共步骤统一准确holder binding、原seal幂等、全部secondary登记、staging/primary、Job、fresh time及截止检查。该提取有三个真实路径，不是为未来adapter造框架。

同Tx负责集合必须仍完整：所有已登记secondary以及未ACK副本/attempt都保留。新seal禁止新增复制/出版资格；已发物理写仍由既有文件fence与原责任处理，PG行锁不冒充停止文件效果。若已存在同一准确政策seal，沿原identity恢复；若是不同政策/主动seal，不能覆盖其来源key、deadline或擅自把新责任挂成已确认，当前窄分支保留pending/明确冲突，关联扩展由真实需要的后续tracer完成。

所有入口都须在实际阻塞工作后，以DB freshNow核当前管理权限与执行截止；整页写完再检查所收集的最早有效执行界限，失败回滚。不凭函数入口now判断最后保存仍合法，不增新的全局锁序/事务框架。

## 4. 原deadline不刷新

政策seal使用**责任自己的原Deadline**，不是候选Change已进入另一自然phase后的新deadline，也不是now+WorkBudget。它仍需满足现有seal执行条件：当前未到期、在当前有限管理能力与允许WorkBudget范围内。若当前配置/TrustedUntil不足以承接原窗口，则保留pending/原原因或明确拒绝；不改短原责任期限来伪造同一事实。

原维护预算已耗尽时：**不新建能产生物理效果的seal/Job，不续原lease，不从ExpiryDeadline借未来预算**。保持原pending/residual、负责方与原时间；若需要保存“原执行窗口已耗尽”说明，也只能独立准确qualification保留所有历史字段，不宣称清理完成。上一决定允许过期之后在当前有限授权下核对restored-save为not_required，因为它不发物理效果；这个例外不能搬到seal路径。

已seal后预算过期或Claim未知，继续封新使用并保留未确认holder/原责任pending，不能撤seal或以退款/宽policy复活。后续若要新的清理授权必须是显式新有限责任，不在本tracer暗自刷新旧身份。

## 5. Step何时可ACK原政策责任

沿现真实流程：Tx外FenceAndErase后独立ObserveErasure，或独立PG staging观察；准确Identity、fence/erased、完整attempt分页、空NextCursor、无effectErr才把相应holder确认。随后在同owner Tx，准确seal/当前Claim/原Deadline/当前管理资格都通过，且 **AllBodyHoldersErased**为真，才调用新全ACK端口并Complete原Job。责任ACK和Job完成同Tx；任一commit_unknown通过原seal/change重开核对，不返回猜测成功。

`AuthoritativeBodyErased`仅staging+primary，可能允许合法metadata gone；它不是ALLholders。真实secondary仍pending/residual或离线时，即使primary已gone，原政策责任仍pending。不得将manager旧holder布尔、文件not_found、进程exit或后来读到gone当全ACK。

现Step有两个Complete出口：没有待处理holder且AllBodyHoldersErased，以及处理本次holder后的complete分支。两处均须共用准确的“最后当前时钟/Claim/原seal校验→原政策ACK（非空key才做）→Complete”，避免恢复到已ACK全部holder时遗漏原责任或通过较早now。最后责任锁等待后再fresh-check，过期则rollback本次ACK；已落盘的旧物理确认保留，不伪造原期限内完成。主动seal空key只完成自身holder/Job，不寻找相似policy或更新任意政策责任。

## 6. 首tracer资格与后续必要边界

首source savefalse正常tracer应真实观察：原pending仍可读正文→Consume提交同原deadline的seal且正文尚真实存在→新body Get封闭→普通Content.Step不冒充清理owner→重开真实Lifecycle.Step→独立staging/文件确认→所有holder完成→原policy责任erased但全部历史字段不变→metadata-only准确gone与原Command/Publication历史保持。

本条通过只覆盖该正常链。后续分别验证restored-save不seal、read/process-only不强删、跨subject/用途拒绝、祖先因果、原deadline已过/锁等待跨界、CAS变更、整页回滚、多页/secondary pending、重开及确认未知。不要用一条first green替代这些边界，也不因当前仍pending提前写通用框架。

ObjectHolder在权威gone时清false与迟到Finish不重建true仍按上一决定独立tracer处理，本报告不把它混入已通过的restored-save证据。全片metadata/广告与验收状态不变；无新公共合同、原receipt变化或ADR要求。
