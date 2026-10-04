# 04 正式六票复核 — f706fe4

2026-10-04。固定审阅 `f706fe4a61e091c4a7d28bd31137a7fc0ffed6e5`，只读 Git objects。**结论：正式票据符合已采用粒度、真实依赖与完整上下文接法；无必要的新领域决定或票据修正。首票可继续其独立 put/get 示踪链。** 本次不是04实现或验收审查。

## 实际范围与前置资格

全文读取该 pin 的六个 issues、spec、decisions、final-api-handoff、ticket-review、api-adoption.json。另实际读取 `git diff --name-only 1aa21fc…5fbb1a0`，准确为 api-adoption 中12条metadata路径；未改变产品、工具、Schema或测试对象，因此此前1aa具体API判断适用于实际03退出源。没有重新运行或独立联网核CI；47ebce1/run37194868564 success及5fbb1a0正式退出依据root提供并已纳入追踪文档。

三个采用文档的首段都明确：root正式采用、03前置已满足、原pending文字保留为历史、04尚无实现/验收证据。因此其中旧“不得启动”“仍须复核”不是当前新增阻塞。spec已为in-progress；只有01 claimed，其余ready-for-agent并保留真实Blocked by。ready标签不能越过这些边。

## 依赖和每票独立出口

| 本片票 | AC数 | 直接前置 | 正常与拒绝/恢复出口 |
| --- | ---: | --- | --- |
| 01 durable-content-publication | 8 | 无；01–03切片已退出 | 真实PG接纳+对象Sync/回读+公开get/Command；冲突/损坏/缺字节/真实重开，首次有限授权，不等待后票或whole广告 |
| 02 current-source-policies | 7 | 01 | 两源合法派生与真实许可交集；撤权封新使用、持久清理责任及重开，不将物理擦除当本票出口 |
| 03 complete-context-dispatch | 7 | 02 | 真实Content-backed规则Decision全字节回显；各容量/数量拒绝和原Prepared/Publisher超时恢复，完整Snapshot固定 |
| 04 source-change-and-lineage | 6 | 03 | 正常完整编译/Decision；真实源变化、fixtureCAS与排队门禁、两源摘要的披露子集 |
| 05 body-cleanup-and-holders | 7 | 02 | 正常正文清理及gone；实际第二holder残留、迟到安装fence、孤儿/重开，不依赖Snapshot或票04 |
| 06 publication-process-recovery | 6 | 05 | 同gate正常release及真实SIGKILL；原发布恢复或消费票05的准确清理/残留出口 |

实际总计 **41 AC**。图为 `01→02→03→04` 与 `02→05→06`；06需要05是因为自身明确验收“无法合法发布后的原对象清理/残留”，不是形式依赖。没有06→03/04边，没有引入真实Task/Grant/Provider、后续切片或整片CI作为首票前置。

票03的安全基本线按其AC及已采用 final-api-handoff 的当前权限、固定映射、原责任/CAS要求交付；票04深化真实修订/来源竞争及摘要场景，不能解释成票03先允许不受控派发、到票04才补安全。现有正式文字没有要求如此分期，无需加票/加边。

## 原7项spec完整映射

| 原spec AC | 正式票及观察 |
| --- | --- |
| 1 写字节后/发布前杀进程 | 06 AC1–3，消费01真实发布与05合法清理；不返回缺字节published |
| 2 hash/length/version拒绝与正常读取 | 01 AC3–5、7：独立准确字节、版本双身份与固定回执 |
| 3 跨租户/撤权/无交集 | 01 AC6跨tenant基本线；02 AC1–7当前全来源/用途/持久拒绝 |
| 4 强制溢出与完整保留 | 03 AC2–5、7，完整M实际回显、容量/输出/数量派发前拒绝 |
| 5 Snapshot提交前源变化 | 04 AC1–3、5–6，Content自己的当前源检查+fixture自身CAS，明确跨owner限制 |
| 6 实际处理全部来源 | 03 AC7；04 AC4、6，compiler/worker集合分别真实、完整闭包约束披露子集 |
| 7 正文清理/不可回读/残留 | 05 AC1–7，06补故障恢复；真实正文/holder观察而非状态自述 |

原spec AC4的“模型出口零”由正式 adopted decisions §6、handoff §5和票03 AC7共同准确限定：此配置没有真实模型，实际验证规则Component正常有/溢出无请求，不伪造provider调用计数或供应商预算证据。这保留既有批准范围，没有把模型未装配说成真实模型门禁通过。

## 具体接法核对

票03 AC2/3/4/6/7与 final-api-handoff一致：完整canonical mandatory-context/1作为第一材料；原1.1 rule/2 candidate_result真实读取并完整回显；外壳/lock/manifest及输出走真实Content，不偷扩1.1、不新建1.2 Decision、不用旧Seed正文库替验收。1.2仅Content及其所需Command读取；旧合同、Prepared和fixture计费不变。

强制字段覆盖Goal、全部必要条件、control、fixture budget/deadline、未结责任（正常例含非空unknown）、来源和准确策略；compiler与worker实际processed明确分开，经真实Content闭包关联。容量计入元数据重复字节与回显/Proposal，8KiB不被当成允许近56KiB的第一材料。当前权限、原key/bytes、同owner短Tx、fixtureCAS、跨owner非原子与旧装配不热换都未丢失。

## whole关闭与范围

ticket-review明确root在六票resolved后独立做全片审查、完整库存/广告、旧版回归与准确CI；每个issue的Comments又明确独立垂直出口。票01交付可验证的本地公开链时完整profile仍不提前广告，二者不矛盾；不能要求票01等待全部六票才resolve。票06只负责自己的故障链，不背整片结束。

未改仓库、未运行build/test/数据库/服务、未占实施测试槽。未复查或触碰任何历史未知资源；未开启其他票。当前无新增用户确认或外部前提。

