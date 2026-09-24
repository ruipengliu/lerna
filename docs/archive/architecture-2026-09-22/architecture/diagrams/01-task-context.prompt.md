# 多端摘要任务插图

- 正文：[01 项目定位、目标与边界](../main/01-system/01-purpose-and-scope.md)。
- 图片：[01-task-context.png](01-task-context.png)。
- 生成方式：内置 imagegen，先生成新图，再修正背景与文字对比度；选定 PNG 直接保存到仓库。
- 用途：帮助读者辨认资料来源、成果位置与逻辑协作关系，不表达部署拓扑或执行时序。

维护时核对：手机上的《会议记录》、电脑“项目资料”中的《需求说明》、电脑“输出”中的《项目进展摘要》位置正确；手机收到文件位置与结论来源。图中的双向连线只表示逻辑交互，Harness 与模型没有固定部署位置。授权、在线前提与读回核验要求由正文说明。

## 生成提示词

```text
Use case: infographic-diagram
Asset type: an illustrated explanatory figure embedded in a Chinese technical architecture chapter; a new raster illustration, not a UI screenshot.
Primary request: Help readers immediately understand ONE project-summary task spanning a user's phone and computer, coordinated by Harness with help from a model. Show where input documents are, where the finished file is saved, and where the user receives the result.
Style/medium: polished editorial device illustration with precise infographic typography, front-facing phone and laptop with subtle depth; calm white background, restrained accent colors, generous whitespace. Large, legible Simplified Chinese sans-serif text. Landscape composition, approximately 1536×1024.
Composition:
- A short title centered at the top: “一项任务，连接手机与电脑”.
- On the left, a clearly recognizable phone illustration labeled “手机”. On its screen are three spacious, clearly separated areas: a user request bubble “整理项目进展”; an input document card “会议记录”; and a result notification “文件位置与结论来源”. The notification is text only, not the saved summary document.
- In the center, a prominent logical coordination card labeled “Harness” with the subtitle “协调行动 · 保持进度”.
- Above the Harness card, a smaller, visually distinct card labeled “模型” with subtitle “理解 · 推理 · 生成”. Connect this card to Harness with one vertical bidirectional arrow.
- On the right, a recognizable laptop illustration labeled “电脑”. Its screen has two clearly separated folder areas: “项目资料” containing a document card “需求说明”; and “输出” containing a document card “项目进展摘要” with the status “已保存并核验”.
- Connect the phone and Harness with a clean horizontal bidirectional arrow; connect Harness and the computer with a second clean horizontal bidirectional arrow. These are logical interactions, not a chronological flow. Keep connectors outside device screens and avoid crossings.
Text (verbatim, all text to appear): “一项任务，连接手机与电脑”, “手机”, “整理项目进展”, “会议记录”, “文件位置与结论来源”, “Harness”, “协调行动 · 保持进度”, “模型”, “理解 · 推理 · 生成”, “电脑”, “项目资料”, “需求说明”, “输出”, “项目进展摘要”, “已保存并核验”.
Constraints: Place 会议记录 ONLY on phone; 需求说明 ONLY in the computer's 项目资料 folder; 项目进展摘要 ONLY in the computer's 输出 folder. The phone receives the file location and sources, not another copy of the summary. Both devices belong to one user. Harness and the model are logical responsibilities; do not show a cloud, server location, network topology or per-device Agent. Every Chinese label must be exact, readable, and unobstructed. Make document labels particularly large. Do not add tiny filler text, logos, watermarks, robots, decorative charts or unrequested text.
```

## 背景与对比度修正

初稿背景过暗，标题对比度不足；在初稿图上应用以下编辑提示词，保留布局、标签与交付位置。

```text
Use case: lighting-weather
Asset type: revision of a Chinese educational infographic for a Markdown architecture chapter.
Edit target: the attached generated phone–Harness–computer infographic.
Primary request: Correct only the background, lighting and text contrast so the diagram reads clearly on a white documentation page.
Change: Replace the entire dark vignette and all smoky gray/black background gradients with a uniform clean WHITE background. Remove the strong glow around objects. Retain only very subtle, light-gray contact shadows under the phone and laptop. The title at the top must be crisp DARK NAVY or near-black on pure white, fully visible, with exact text “一项任务，连接手机与电脑”. Make all other existing labels crisp and high-contrast.
Invariants: Keep the exact same landscape composition, device illustrations, cards, positions, folder ownership, logical bidirectional arrows, and all existing Simplified Chinese labels and English “Harness”. Do not move, add or delete cards, labels, devices or arrows. 会议记录 remains on the phone; 需求说明 stays inside the computer's 项目资料 folder; 项目进展摘要 stays inside its 输出 folder; the phone reports only 文件位置与结论来源. The computer's status must remain “已保存并核验”. Preserve the same illustration, correcting legibility rather than redesigning it.
Avoid: Any dark vignette, any dark background, heavy shadows, new text, watermarks, clouds or decorative elements.
```
