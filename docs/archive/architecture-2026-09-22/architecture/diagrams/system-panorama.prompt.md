# Harness 系统全景插图：生成提示词与核对记录

本页与 PNG 保留为历史版本。当前入口为[可编辑 draw.io 全景图](system-panorama.drawio)和[SVG 预览](system-panorama.svg)；新版文字、几何与导出方式见[图示构建说明](../../../scripts/architecture/README.md#系统运行全景图)。下述核对记录仅描述历史插图。

- 图片：[system-panorama.png](system-panorama.png)，1536 × 1024。
- 阅读入口：[系统运行全景](../main/01-system/03-component-panorama.md)。
- 生成方式：内置 imagegen；一次生成、一次关系修正、一次局部文字修正。选定 PNG 原样复制到仓库，没有用代码重绘或覆盖图中文字。
- 用途：通过中文动作说明和手机／电脑摘要任务解释系统流程；不表达部署层、共同事务或已实现能力。
- 维护来源：[写作约定](../README.md)、[范围与覆盖](../maintenance/content-map.md)、[处理项内容源](../../../scripts/architecture/system-flow-content.json)。完整字段与事务规则仍归对应正文。
- 配套：[可缩放机制核查图](system-flow-map.html)保留精确处理项、事务标记和正文定位；主图始终显示在单张图片中。

## 已核对的内容

| 核对点 | 选定图中的表达 |
| --- | --- |
| 输入与结果位置 | 手机会议记录、电脑需求说明；摘要保存在电脑，手机收到文件位置与结论来源。 |
| 准备与决策方向 | 内核向上请求准备资料、请求决策；上下文向下返回本轮资料，大脑向下返回提案。 |
| 大脑与执行边界 | 大脑只提方案；内核确认记录存稳后派发，执行端再次检查权限和控制。 |
| 启动与等待 | 先记下启动准备，再调用工具；下次核对被保存，到期取得资格后查询原操作，未完成则留下下一轮。 |
| 两种未知 | 不确定记录是否保存时查原提交；不确定动作是否完成时查原操作，超时不是盲目重做的理由。 |
| 核验与完成 | 保存效果已确认后安排读回；核验、必要交付和在途处置齐备才完成。界面呈现不代表用户批准。 |
| 协作与端云 | 子任务独立运行、父任务核验汇总；原端停止推进，新端确认接手；离线条件满足才继续，重连先核对。 |
| 实现与改进方向 | 扩展向内核提供实现和配置；获准的改进进入扩展，不直接向执行系统下发用户动作。 |
| 中文与视觉 | 已目视检查标题、中文动作句、箭头标签、等待回路与页边界；生成后将资料交接的两处反向标签单独修正。 |

“保存已确认”指文件保存效果已获确认；“到期取得资格”还受当前权限、控制、预算、依赖和执行资格约束；“确认存稳”包含适用的耐久确认。“原端停止推进，新端确认接手后继续”概括移交条件，检查点准备、原端封存及目标激活分别按第 12 篇处理。“单机与端云沿用同一规则”表达行为契约一致，不代表所有运行位置具备相同能力。图示核对不构成业务、容量或故障恢复验收。

## 初始生成提示词

```text
Use case: infographic-diagram
Asset type: a new, finished raster infographic for a Chinese technical architecture guide.
Primary request: Generate ONE complete, highly readable Harness system panorama. The previous diagram was full of API names, abbreviations and long paragraphs; make the actual behavior understandable in clear Simplified Chinese. This is a technical explanation through a concrete phone-and-computer task, with visible internal mini-flows, not a decorative poster and not a vague collection of icons.
Canvas: high-resolution landscape, approximately 3:2, ideally 3840 x 2560 or the highest supported resolution. White background. All content fits in ONE image with generous margins.
Visual style: polished editorial infographic, crisp vector-like drawing rendered as a bitmap, clean Chinese sans-serif typography, dark navy text, calm blue main arrows and borders, pale blue fills, restrained amber for uncertain/waiting cases and green for verified delivery. Simple recognizable phone, laptop, document, checklist, clock and shield illustrations. No robots, no 3D, no glow, no textures or heavy shadows. Large typography is the priority: body copy should be about 1/80 of the canvas width, large section titles about 1/45, and a clear larger main title. Do not shrink type to fit; use concise labels and spacious layout.

Composition and topology:
Make a connected collaboration diagram on one open canvas. No architectural layers, no separate numbered panels, no dashboard, no collapsed detail. Main visual emphasis: a phone/user on the left, a large TASK CORE in the center, and a large EXECUTION area on the right. A context/knowledge area and a brain/model area sit near the core and form a short, easy-to-trace preparation-and-proposal loop. The user request goes to the core; the core coordinates context preparation and the brain; the brain returns a proposal to the core; only the core's checked and saved work goes to execution. Execution returns observed facts to the core. After read-back verification and completion checks, the core sends the formal result to the phone. The brain must have NO direct action arrow to tools.
Below/around the actual responsibility they affect, place smaller authorization/control, collaboration/edge-cloud, extension/runtime and observation/evaluation areas. These are supporting mechanisms with thin, unobtrusive labeled connections, not mandatory sequential task stages. Keep all of them visible. Use the available space to avoid crossing arrows. Main request/proposal/action/report arrows have clear heads and short labels; secondary configuration/evidence links use a lighter dashed stroke. Do not make readers follow a web of lines.
Inside the execution area show a real visible waiting loop with arrows and an exit returning facts to the core. Put the two unknown-result rules near the relevant core/execution area, as short amber callouts rather than long crossing lines.
These areas must clearly show their own visible short action sequences, not merely module names.

Exact visible text, grouped by area (render these Simplified Chinese labels accurately, with short lines; do not add dense filler or English identifiers other than Harness):

TITLE
“Harness 系统全景”
Subtitle: “从提出任务，到执行核验，再把结果交给用户”

USER / PHONE, left
Heading: “用户与界面”
Request bubble: “整理项目进展”
Small input captions: “手机：会议记录” and “电脑：需求说明”
“新建任务，或补充当前任务”
“确认、暂停、取消交给对应模块”
Result on the phone: “文件位置与结论来源”
Near the result: “界面已显示，不等于用户已批准”

CONTEXT / KNOWLEDGE, near the core
Heading: “上下文、记忆与能力”
“整理本轮资料，保留来源”
“按权限读取偏好与历史”
“查找工具，核对参数与恢复能力”
“资料不足：先安排读取，再判断”
“读取资料不自动变成长期记忆”

BRAIN / MODEL, above or beside the core
Heading: “大脑与模型”
Mini-flow: “理解资料 → 提出下一步”
“提出行动、等待或结果建议”
A prominent short boundary label: “只提方案，不能直接执行”

TASK CORE, dominant central area
Heading: “任务内核”
Mini-flow: “接收输入 → 记住进展 → 安排下一步”
“检查最新要求、权限和预算”
“任务变化和后续工作一起保存”
“确认存稳后，才把工作交出去”
“需要新判断时，再调用大脑”
Verification mini-flow within the core:
“保存已确认 → 安排读回 → 核对内容”
“核验、必要交付和在途处置齐备，才完成”
Nearby short callout: “要求或资料变了：重新核对旧建议”

EXECUTION, dominant right area
Heading: “执行与效果核对”
“动作发出前，再检查权限和控制”
Mini-flow: “记下启动准备 → 调用工具 → 保存观察”
Small phone/laptop/API pictograms are optional here, but no new text labels.
Concrete action caption: “电脑：保存摘要，再按安排读回”
A prominent boundary label: “已收到、已受理，都不等于已做成”
Local subheading: “等待怎样继续”
Visible cyclic mini-flow:
“存好下次核对 → 到期取得资格 → 查询原操作 → 保存新事实”
A short backward loop label: “仍未完成，安排下一轮”
A completed-result exit label: “新事实交回内核”
Two clearly distinct small amber callouts at the core/execution boundary:
“记录是否存好不清楚：查原提交”
“动作是否完成不清楚：查原操作”
“不能把超时当作重新执行的理由”

AUTHORIZATION / CONTROL, connected to the core and execution
Heading: “授权与控制”
“缺许可先询问，处理时再检查”
“暂停先挡新工作；在途动作继续处置”
“取消后仍核对实际停止和已发生的效果”

COLLABORATION / EDGE-CLOUD, connected to the core
Heading: “协作与端云”
“子任务独立运行，父任务核验汇总”
“原端停止推进，新端确认接手后继续”
“离线条件齐备才运行，重连先核对”

EXTENSIONS / RUNTIME, connected lightly to relevant implementations
Heading: “扩展与运行保障”
“模型、记忆和工具按接口替换”
“新工作用新版本，在途保持原版本”
“单机与端云沿用同一规则”
“按用户隔离；重启接着原工作”

OBSERVATION / EVALUATION / EVOLUTION, connected to evidence and to extension activation
Heading: “观测、评测与改进”
Mini-flow: “收集运行证据 → 独立评测 → 获准后分阶段启用”
“监测变化；异常时停止或有条件回退”
“代码发布由维护者确认”
Clearly show that approved changes flow to extension/runtime, NOT straight to executing user actions.

Small bottom note, still readable:
“各模块分别保存自己的记录；模型和外部动作在保存记录的事务之外。”
“示例前提：资料与工具已获授权；执行时仍须检查当前条件。”

Allowed short arrow labels: “请求”, “准备资料”, “本轮资料”, “提案”, “已保存的工作”, “执行事实”, “核验后交付”, “检查与控制”, “委派与结果”, “实现与配置”, “运行证据”, “获准的改进”.
Use only a useful subset; every arrow must match the topology above.

Accuracy constraints:
The summary file is saved on the COMPUTER; the PHONE receives the location and sources. Do not place a completed summary document on the phone. Context contains acquired information, and missing information must lead to a read action before it can be used. The brain does not execute tools or decide durable business truth. “保存已确认” refers to confirmed file-save effects before read-back; no completion before checking evidence. Waiting is persisted work repeatedly claimed and checked, not a forever-running model call, and querying the original operation is not starting it again. A timeout is uncertainty, not permission to redo a side effect. Parent/child tasks and modules commit independently; show no giant transaction around the whole system. The owner handoff sentence is a concise condition, not a claim of a cross-node atomic transaction. Do not show support modules as mandatory steps after each task. Avoid identifiers such as TX, Owner, Inbox, Outbox, state_version, S/R/J, API method names, or unexplained abbreviations. No hidden detail, no tiny footnotes, no watermarks, no additional slogans.
```

## 关系与执行准备修正

在初稿上执行以下编辑。首次编辑修正了决策、执行与扩展的主要关系，但资料交接的两个标签仍未互换，因此追加下一节的局部修正。

```text
Use case: precise-object-edit / infographic-diagram semantic correction.
Edit target: the supplied Chinese Harness panorama. Preserve its overall composition, nine responsibility areas, clean white-and-blue style, large legible Chinese typography, all accurate text, and the visible waiting loop. This is a focused correction of handoff semantics and one omitted execution preparation step. Do not redesign or add new modules.

Make exactly these corrections:
1. Between “上下文、记忆与能力” and “任务内核”, correct the two labels without reversing the arrows: the DOWNWARD arrow FROM the context area TO the core must be labeled “本轮资料”; the UPWARD arrow FROM the core TO the context area must be labeled “准备资料”. They are currently swapped.
2. Between “大脑与模型” and “任务内核”, keep the downward proposal arrow labeled “提案”. Change the upward arrow's label from “已保存的工作” to “请求决策”. The core asks the brain to decide; the brain returns a proposal.
3. The main RIGHTWARD arrow FROM the core TO “执行与效果核对” must be labeled “已保存的工作”, replacing the incorrect “委派与结果”. The LEFTWARD return arrow stays “执行事实”.
4. Reverse the dashed connector between “扩展与运行保障” and “任务内核” so that its arrowhead points at the CORE, not the extension area. Keep the label “实现与配置”. Extensions provide implementations/configuration to the runtime.
5. Remove the upward dashed arrow FROM “观测、评测与改进” into the bottom of the execution area, and remove the “获准的改进” label from that vertical position. Approved evolution MUST NOT directly start user actions. The EXISTING LEFTWARD dashed arrow FROM evaluation/evolution TO “扩展与运行保障” is the correct destination: keep it and put the short label “获准的改进” beside that horizontal connector, with no overlap. Keep the downward runtime-evidence link to evaluation unchanged.
6. In the laptop drawing inside execution, retain the file title “项目进展摘要” but REMOVE “（已保存）”; the surrounding figure is also explaining an unfinished operation, so it must not prematurely declare success.
7. Add the omitted short execution mini-flow visibly beneath the current-permission check, BEFORE the laptop/caption/waiting sequence: “记下启动准备 → 调用工具 → 保存观察”. If space is needed, make only the laptop illustration slightly shorter; do NOT shrink body text or the waiting loop. The purpose is to show that restart-safe preparation is saved before any external side effect.
8. In the evaluation mini-flow, replace “独立评测” with “隔离评测”. Keep “收集运行证据” and “获准后分阶段启用”, and the code-release approval line.
9. In the phone result inset, ensure the line reads exactly “文件位置与结论来源”; do not omit “结论”.

All other text and arrow directions are invariant. Preserve the no-direct-brain-to-tools boundary, the original-operation waiting loop and its return to the core, the separate original-commit vs original-operation unknown rules, completion only after checks, phone receipt vs computer file location, authorization/control semantics and separate-module transactions. No API names or extra identifiers; no new arrow from evaluation to execution. Use clean accurate Simplified Chinese text, no watermark. Return one corrected complete panorama at the same aspect ratio and at least the same resolution.
```

## 资料交接标签修正

只将向下箭头旁的文字改为“本轮资料”，向上箭头旁的文字改为“准备资料”，保留其他内容。

```text
Use case: text-localization.
Edit target: this finished Harness panorama.
Perform ONLY TWO small text replacements, leaving every other pixel, every arrow shape and direction, all boxes, illustrations, remaining text, layout and resolution unchanged.
The narrow gap directly BELOW the “上下文、记忆与能力” box and ABOVE the “任务内核” box contains two blue vertical arrows and two four-character captions.
1. The LEFT caption, at approximately x=438–515, y=325–349 in this 1536×1024 image, currently reads “准备资料”. REPLACE THIS LEFT CAPTION with exactly “本轮资料”.
2. The RIGHT caption, at approximately x=586–663, y=325–349, currently reads “本轮资料”. REPLACE THIS RIGHT CAPTION with exactly “准备资料”.
This swaps the two text labels IN PLACE. Keep the left arrow pointing DOWN and the right arrow pointing UP. The finished physical LEFT label must be 本轮资料, and the finished physical RIGHT label must be 准备资料.
The reason: context sends this round's materials down to the core; the core asks upward for preparation.
Make no changes anywhere else. Preserve identical blue type size, weight and white background. Do not move the two arrows. Do not change the neighboring brain labels “提案” or “请求决策”. No new text, no watermark.
```
