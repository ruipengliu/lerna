# 概念图原则说明修订

用户要求：原则短语不够直观，可增加少量说明。使用内置 imagegen 编辑现有项目图片；原则改为两行四列，每项使用直白标题与一句解释，保留建模方法与模块关系。

## 完整编辑提示词

```text
Use case: infographic-diagram edit.
Edit the supplied existing Harness concept image to make its DESIGN PRINCIPLES immediately understandable. The user explicitly allows a little explanatory text when a short phrase is unclear.

Change ONLY the design-principles section and its necessary vertical spacing. Preserve the image's white paper background, restrained navy/blue/orange palette, title, six-step modeling method, complete nine-module relationship diagram, topology, module names and footer. The bottom two sections must remain intact and readable. Do not introduce a new design, modules, arrows, API details, or decorative machinery.

Layout: replace the current single row of eight cryptic labels with a spacious TWO-ROW, FOUR-COLUMN principles band. Read left-to-right, then top-to-bottom. Reuse the same eight simple principle glyphs in their original sequence. For each principle place a modest glyph above or beside a bold plain-language title, then ONE concise explanatory sentence in smaller, highly legible Chinese type. There should be no paragraph cards or heavy filled rectangles. Use consistent alignment and generous column gutters. Explanatory sentences may wrap into two balanced lines. Increase the canvas height as needed; a slightly taller landscape or 4:3 canvas is preferred to shrinking any text. The module diagram should still be large and legible.

Render the following eight title-and-explanation pairs EXACTLY in Simplified Chinese, with no paraphrase, omitted characters or additional words:

ROW 1, COLUMN 1:
Title: “谁裁决，谁保存事实”
Explanation: “任务、许可和效果各有负责方”

ROW 1, COLUMN 2:
Title: “模型提案，运行时准入”
Explanation: “行动前检查控制、权限与预算”

ROW 1, COLUMN 3:
Title: “决定与后续责任共同保存”
Explanation: “确认决定前一起落盘，重启后继续处理”

ROW 1, COLUMN 4:
Title: “恢复沿用原身份”
Explanation: “关闭后仍保留身份，防止旧请求复活”

ROW 2, COLUMN 1:
Title: “不同结果，分别确认”
Explanation: “接纳、效果、控制、费用、完成分别记录”

ROW 2, COLUMN 2:
Title: “固定版本，检查当前资格”
Explanation: “引用准确版本，使用前仍要检查资格”

ROW 2, COLUMN 3:
Title: “自治有上限，未知有负责方”
Explanation: “自动尝试有界，原负责方保留核对责任”

ROW 2, COLUMN 4:
Title: “实现可替换，行为须一致”
Explanation: “成功点、错误语义和恢复规则保持一致”

Keep the section heading exactly “设计原则”. Retain “建模方法” and “模块关系” below it and preserve their existing correct content. There must be exactly eight principles, each clearly pairing its own title and explanation. The explanatory lines need visibly lighter weight, but sufficient contrast. Prefer clarity and large readable typography to compactness. The final image should feel calm, polished and explanatory, with no dark patches, glows, shadows, text clipping or line collisions.
```

## 输出与检查

- 使用内置 imagegen 编辑，最终输出为 `exec-36b42cc7-ea6d-4324-b793-93d6b793fe8d.png`。
- 已更新 `docs/architecture/diagrams/design-concepts.png`，1536 × 1024 PNG，并同步总览图注和审查记录。
- 实际查看八项原则标题与说明，核对文字、排版及与总览的语义对应；其余图示仍表达原有模块职责关系。
