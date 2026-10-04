**正式采用：2026-10-04。** Root全文读取授权 Astra high 的必要裁决并采用，交由唯一LOCAL执行owner实施。以下是设计及验证要求，不是WIP正确性或实际测试通过证明；02仍claimed、7AC未完成。

# 04 票02：旧 oracle 调整与准确政策责任分类

2026-10-04。依据已正式采用的 `f54d697b50d63490a2ed573b8dd91c18d3178110:.scratch/lerna-04-content-snapshots/ticket-02-handoff.md`；票01检查点 `7c0bce515f7b2fa443e2d00e1b14cfee22dccfa9` 已退出，02基线 `f05b2f1068958ba6b63e6c1dc58d6cd1684446cc`。核对旧 `14ead831b8834892da5ff13a9b883494247f327f` 两个测试；它们到01交付 `1a7d910238eb74cddc712d92b0ba4014a72ff507` 无差量。只读 WIP management.go 的相关分支作接法背景，**不是固定02实现审查或正确性证明**。未执行 native/build/test/DB。

**决定：采用两个测试 oracle 的窄调整，同时补准确政策失配的责任分类。** Root报告的 affected-first native1/group absent 中这两处旧预期，与已采用02行为不相容；不能据这两条断言失败认定实现错误，也不能只改断言就宣称实现正确。新 oracle 必须保留下面的公开责任、原历史与独立正文观察。

## 1. Step processed=true 不再意味着关联重开出版

旧 `content_association_test.go:48–49` 在发布来源的 process/save 撤销后，断言一次 Step 必须 processed=false。首票只有 publish phase 时它是局部控制；02政策更新已必须同Tx登记传播 Job，Step 现可合法处理这个原政策工作。因此“没有任何工作”不能继续用作“关联没有新增出版责任”的 oracle。

**采用修正：** 去掉这一处 `processed == false` 条件，仍要求调用无未知错误；也不要改成无条件断言某一次必须 processed=true。通过受信管理入口取得原 policy-change 身份，有限推进并分页 ObserveChange，确认处理的是其原传播责任。不要用私表 Job 数或内部调用次数判业务行为。

保留并加强原题的公开判定：新同声明 normal alias 合法；源 process/save 失效后的新 alias 固定 forbidden；原 Command 重放 receipt 字节不变；原 Command get 的 progress 仍 published，不能倒回 preparing或产生新版本；独立源/派生对象仍是原准确三份字节，不能只比较目录数量。process/save-only 撤销且 read/disclose/当前期限仍有效时，独立 Get 正常对照继续可读。传播/清理 pending 是另外的责任，不授权票02删正文或把历史出版抹掉。

必要区分：允许处理已登记 policy_propagation **不允许**拒绝的关联创建 publish Job、重传对象或产生新attempt。这些非法行为仍应由原身份/进展/独立字节和实际管理观察发现，不能把整个断言块删除后失去证明。

## 2. 已过期的耐久 cap 不被较新宽政策复活

旧 `content_direct_read_test.go:62–63,123–130` 的 expired 分支先把 source RetainUntil 改到过去，再放宽并要求原 derived ref 可读。01只在读取时核当前政策；02正式管理步骤还必须把适用原 source/后代的 current cap 单调收紧并登记责任。更高 policy revision 能更新许可，但不能延长已接受版本的耐久 current cap。

**采用修正：** 仅将 expired 分支的旧 ref 恢复可读预期替换为：放宽后原 source 和原 derived 仍 rejected expired；重开后仍拒绝，原 receipt/published 历史保留，管理观察显示原责任且不宣称 erased。传播未扫到后代时，当前 Get 的完整闭包仍须读取已收紧的 source cap而立即拒绝，不能等批次追上才生效。未到期但已收紧的 cap 也不得被恢复为旧宽值。

read/disclose 布尔撤销分支保留恢复准确许可后的可读正常对照；它们不能被一律改成永久关闭。process/save-only 正常对照也保留。动作许可恢复与绝对 cap 恢复不是同一件事。

新增正常出口必须是真实新准确版本链：用获准输入提交新 source 版本及新 Command，独立发布/读回，再以该新 source 建新 derived 版本和新 Command、发布/读回。不得从已过期旧 source 再派生，不得更改旧 version/hash绑定、旧 command payload或旧 retain_until来“刷新”；也不得通过测试代码偷偷读取过期旧字节生成新来源。可使用本测试本来合法持有的独立输入字节，明确新政策/保存资格，真实走公开 put/Step/get。

## 3. 兼容 malformed-fullRef fixture：无有效许可必须有相应责任

新的受信 Manager 安装对存在版本的错误完整绑定仍应拒绝。为保留票01的公开否定场景，兼容 `InstallFixturePolicy` 可明确允许安装 malformed-binding fixture；它**仍必须走同一政策变更/责任事务**，不能只换行而跳过分类。

A为实际不可变版本，B与A有相同 owner/id/version 但 hash/media/length 不同。如果同主体/同用途原 policy A 被 fixture policy B 替换，则其 true flags 也**没有授予实际 A 任何动作**。责任分类必须在准确资源绑定层先判断，不能只用布尔从true→false差量。

**采用默认：** 对实际 A 登记受影响动作完整集合 `read, process, save, sync, disclose`，保留变更原身份/revision/准确责任 Ref A和失配原因；原同主体/用途的保存依据失效，已知 staging/object/原未确认 attempt 均应有 body-cleanup pending/holder-unconfirmed责任。是否实际已存在字节/是否可删除不由 flags 或本次门禁推断；票02不物理删除、不报 erased。

传播到后代时，必须比较 **changed policy B 与它所替代/影响的实际祖先 A**。不能比较 B 与后代 target D 的 Ref：祖先与后代本来就不同，这种比较会把所有合法派生误判为绑定错误。用原 Job所绑定的准确 source Record/不可变来源身份，在其短 Tx核出失配，再按原 change 将该事实用于分页责任；不靠当前“最新正确政策”覆盖历史 change，也不单凭 Previous.Ref 猜实际 source。当前已有 Record/PolicyChange/责任形式足以表达，不需要新通用ADT或Grant。

**主体/用途限制：** 只有责任 record 的原保存主体和用途确实依赖这个 policy key（直接或完整已登记祖先）时，失效保存依据才使其正文清理 pending。另一个 Subject 或另一个 Purpose 的失配，只封其相关使用并登记 use-review，不授权全局删除、不收紧不相关 writer 的全局 current cap，也不能清掉其他人的存储责任。整体错误绑定使该 policy key 对实际源五动作均不成立，并不意味全系统五动作被全局撤销。

区分正常动作收紧：单独撤销 read/process/disclose 不等同撤销 save；仅撤销 save可登记原保存责任 pending，但独立 read/disclose仍按各自当前资格裁决，不能为了让新清理测试通过就把所有 Get 都改成拒绝。RetainUntil真正到期则是适用 cap 失效，按第2节不可被新宽政策复活。

## 4. 当前 WIP 的使用资格与验证要求

只读看到 WIP `affected(change, now)` 目前主要按 action flags/ValidUntil 和 Save/RetainUntil 分类；单凭该分支不足以证明 full-ref 失配已纳入。`register` 已区分 record.Subject/Purpose，是应保留的范围约束，不是可省略准确祖先检查的理由。本稿不对不断变化的02源码作最终 finding closure。

sole fixer 应先明确修改上述旧 oracle，并补真实管理观察：原 change可重开/全页查，receipt/原出版/独立字节不变；expired旧链永不因 widening 复活，新合法链能完整正常完成；兼容错误完整ref使同主体/用途的五动作无效且保存责任 pending，而不同主体/用途对照不产生不当正文删除责任。恢复准确政策不擦掉此前已登记未确认 cleanup，后续真实核对决定其状态。

旧两断言失败按“02已采用语义引起的测试预期调整”记录，不将其伪报为新实现 business red。准确绑定责任分类若当前实现确有缺口，则另提供对应公开管理责任场景的实际 red→green；不得把前面的预期失败借作该缺口的证据。全部仍需正常对照、有限退出与原 native责任确认，最终结论等固定实现和实际执行记录。
