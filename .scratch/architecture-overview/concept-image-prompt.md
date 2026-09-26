# Harness 抽象概念图生成提示词

生成方式：内置 imagegen；生成全新项目图片，未使用参考图或编辑已有全景图。

```text
Use case: infographic-diagram
Asset type: A polished abstract architecture concept illustration for the existing Harness technical design documentation, not a detailed implementation diagram.
Primary request: Generate ONE refined landscape concept map, with beautifully legible Simplified Chinese text, showing DESIGN PRINCIPLES, MODELING METHOD, and MODULE RELATIONSHIPS together. Use generous whitespace, precise visual hierarchy and elegant geometric symbolism instead of dense text or software component cards.

Style: contemporary editorial information illustration, flat vector-like ink drawing with restrained soft geometric forms on a warm white / very light gray paper background. Reuse the established document colors: deep charcoal #2d3142, slate #4f5d75, muted blue #2e5aa8, and sparing warm orange #eb6c36. No neon, no dark dashboard, no photorealistic robots, no circuit-board decoration, no gradients or 3D glass. Wide landscape, high resolution, beautiful print-ready composition. All labels clean, horizontal and crisp.

Composition: Three connected reading levels. A concise principles band at top, a compact modeling pathway beneath, and a larger, visually dominant module constellation below. Make the whole image feel like a cohesive conceptual atlas rather than three grids of boxed paragraphs. Use simple abstract glyphs, open circles, interlocking shapes and thin carefully routed arrows. Typography and connection clarity are more important than ornament.

Main title (exact): "Harness 架构概念图"
Subtitle (exact): "以事实裁决组织目标推进"

Top level heading (exact): "设计原则"
Eight evenly spaced small abstract glyphs, each with ONE short label; arrange in a balanced band or two gentle rows if needed, with NO descriptions:
"事实有主"
"提案准入"
"决定与责任同存"
"原身份恢复"
"状态分维"
"版本与资格"
"有界自治"
"契约替换"
Suggested glyph semantics: anchored point for authority, a proposal passing a gate, linked durable shapes, returning path to original identity, independent parallel sliders, layered version plus check, bounded orbit, interchangeable puzzle joints. Avoid literal emoji. Glyphs should be visually distinct but share one visual language.

Middle level heading (exact): "建模方法"
One six-step flowing ribbon, using thin arrows and small geometric stations; the exact labels in this exact order:
"目标与约束" → "待判断事实" → "固定裁决者" → "身份与资格" → "独立状态" → "提交与恢复"
Do not create large filled cards. One thin curved return arc from the last station toward the first, labeled "正常链路与异常推演", expressing iterative validation, not runtime message flow.

Bottom level heading (exact): "模块关系"
Nine modules, each a distinct abstract emblem with its exact name and one short role subtitle below. The task runtime is the largest orange focal emblem; execution is a secondary orange accent. Other nodes are charcoal/slate, light-filled or open shapes.
1. "任务运行时" with small "Task Home" and role "任务裁决"
2. "大脑" with role "单轮提案"
3. "记忆与内容" with role "版本与来源"
4. "执行" with role "动作与效果"
5. "应用与交互" with role "目标与输入"
6. "Agent 协作" with role "有界委派"
7. "权限与隔离" with role "许可与额度"
8. "观测、评测与改进" with role "证据与批准"
9. "扩展与宿主" with role "装配与就绪"

Arrange six task-processing modules as a balanced constellation around the central Task Home, with the three governance/hosting modules in a lighter peripheral band. Relationships must be easy to trace:
- Application/interaction connects to Task Home.
- Task Home connects directly and separately to Brain, Memory/Content, Execution, and Agent Collaboration. Use clear paired-direction arrows for request and returned facts, without individual verbose labels.
- Absolutely NO direct arrow from Brain to Execution. Brain offers proposals; Home decides admission.
- Security connects by a dashed governance relation to the task-processing area, representing checks by actual users of permission.
- Task-processing facts flow by a dashed relation to Evaluation.
- Evaluation and Extensions are linked for approval and current readiness facts.
- Extensions connects by a dashed relation to the task-processing area for assembly and lifecycle.
The governance dashed relation may terminate on a subtle shared boundary around the six logical task modules. This is a scope indication, not an extra service; do not invent a hub or node there.
Use only a few short arrow labels where useful: "请求 / 事实", "授权", "观测", "批准 / 就绪", "装配".
Avoid crossings, ambiguous line tangles, arrows through text, duplicate modules, and hidden endpoints.

Very small bottom legend, exact:
"实线：业务交接    虚线：治理与装配"
"Home 裁决任务；各负责方裁决所属事实"
"逻辑模块 ≠ 进程 ≠ 提交域"

Constraints: This is a conceptual illustration, not deployment topology or a database schema. Do not add technical field lists, APIs, class names, long sentences, performance numbers, network equipment, or extra modules. Keep all nine module names once each. Correct Chinese characters, no random text, no watermark. Preserve the above responsibility split. The goal is to understand the design at a glance, with graceful abstraction and enough precise relationships to remain meaningful.
```

## 浅色与可读性修订

```text
Edit this existing Harness conceptual infographic. Preserve its three sections, all nine module names, all design-principle labels, the six-step modeling chain, central Task Home, roles and overall layout. The main correction is VISUAL READABILITY AND STYLE.

Replace the ENTIRE dark navy/gray blurry background with completely flat, solid, opaque warm white (#FAFAF7). Absolutely no dark patches, no blur, no glow, no vignettes, no spotlight effects, no fog, no shadows, no translucent overlays. Treat the image as an elegant flat printed technical illustration on white paper. All text including title, subtitles and footer must be crisp dark charcoal (#2d3142) with high contrast. Lines should be clean slate/blue, and keep a restrained orange accent on the Task Home and small principle motifs. Peripheral module icons should be pale slate disks with NO surrounding glow. Use fine clear borders and subtle flat fill only. The large central logical grouping may use a very pale warm ivory, clearly bounded with a thin dashed line.

Keep all Chinese text legible and correct. Exact principle labels: “事实有主”, “提案准入”, “决定与责任同存”, “原身份恢复”, “状态分维”, “版本与资格”, “有界自治”, “契约替换”. Title “Harness 架构概念图”; subtitle “以事实裁决组织目标推进”. Section headings “设计原则”, “建模方法”, “模块关系”. Modeling labels “目标与约束”, “待判断事实”, “固定裁决者”, “身份与资格”, “独立状态”, “提交与恢复”.

Retain the topology: Application/interaction, Brain, Memory, Execution and Agent Collaboration each connect separately to Home. No Brain to Execution connection. Security provides dashed governance to the central area; observation flows to Evaluation; Evaluation and Extensions exchange approval/readiness; Extensions assembles the central area. In the Home-to-Agent-Collaboration gap only, remove the redundant duplicate curved connectors, keeping just one neat pair of opposing directional arrows. This is visual deduplication, not a new relationship. All other meaningful edges remain.

Do not add paragraphs, data fields, new nodes or decorative illustrations. High resolution landscape, clear airy editorial concept map. Final output MUST have a clean flat WHITE paper background with dark readable typography everywhere.
```

## 标签与关系精校

```text
Final precision edit to this existing infographic. Keep the WHITE background, existing composition, icons, nine modules, design-principle row, modeling chain, font sizes and almost all text unchanged. Only correct the following marked textual and connector details. Render Chinese exactly, with no paraphrasing:

1. Under the left shield node “权限与隔离”, replace its small subtitle with exactly “许可与额度”. The last two characters must be 额 度, not 疏离.
2. Under the bottom-right node “扩展与宿主”, replace its small subtitle with exactly “装配与就绪”. Last two characters 就 绪, not 编排.
3. The vertical dashed relation between “观测、评测与改进” and “扩展与宿主” must have arrowheads at BOTH ends. Its label remains “批准 / 就绪”.
4. In the gap between orange Task Home and “Agent 协作”, erase the two large curved side arrows. Keep just the short centered vertical double-headed arrow linking those two nodes. Do not change other module connections.
5. Replace the ENTIRE bottom legend line below the thin horizontal divider with ONLY these four distinct clean dark-text items, spaced across the width, exact verbatim:
“实线：业务交接”
“虚线：治理与装配”
“各负责方裁决所属事实”
“逻辑模块 ≠ 进程 ≠ 提交域”
The last three characters in the final item are 提 交 域, not 组织边界. Remove all previous wording in that footer before placing these four items. Small line samples may precede the first two items. No additional words.

Do not change anything else. Preserve all other exact readable Chinese labels and the title. Flat white paper, dark legible typography, no shadow or glow.
```

## 删除冗余小标签

```text
Perform a minimal cleanup on this existing Chinese architecture concept image. Do NOT regenerate, redraw, retype, translate, or paraphrase the image. Preserve all pixels, layout, module titles, arrows, title, design principle labels, modeling labels and footer, except TWO tiny erroneous subtitle lines that must be ERASED and left blank.

Erase the small blue subtitle directly BELOW the large correct title “权限与隔离”, at the far left underneath the shield icon (approximately normalized rectangle x=0.060 to 0.135, y=0.750 to 0.779). Keep the larger title “权限与隔离” untouched above it. Leave clean white paper where the small line was. Do not replace it with new text.

Erase the small blue subtitle directly BELOW the large correct title “扩展与宿主”, at the lower right beneath the puzzle icon (approximately normalized rectangle x=0.865 to 0.943, y=0.893 to 0.918). Keep the larger title “扩展与宿主” untouched above it. Leave clean white paper where the small line was. Do not replace it with new text.

These two subordinate captions are not needed. Everything else must remain unchanged. Output a clean, sharp finished image with these two areas blank and no new content.
```

## 最终输出

- 工作区：`docs/architecture/diagrams/design-concepts.png`
- 尺寸：1536 × 1024，PNG。
- 全部生成与修改通过内置 imagegen 完成；没有用脚本改绘图像。
- 最后选用的生成输出：`exec-a77e175f-9a92-46f5-a131-4410e05ca63e.png`。
