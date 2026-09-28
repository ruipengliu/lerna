## 技术方案文档

读者是高级工程师：熟悉分布式系统、事务和常见架构模式，没有参与设计讨论。读完一篇文档，他应能据此实现或评审对应部分。

- **术语**：通用机制使用业界名称（幂等键、transactional outbox、lease、fencing token、tombstone、单写者分区），首次出现给中英对照。项目领域概念以 `CONTEXT.md` 为准；与相近的标准术语有差异时，写明差异。
- **结论先行**：每节第一段给出结论或规则，依据和例外随后。句子主语用具体的组件、表、函数或进程。
- **模块文档骨架**：一句话职责 → 组件与依赖 → 数据模型与状态机 → 关键时序 → 失败处理表 → 取舍（选择、代价、改选条件）。不适用的节省略。
- **保证集中陈述**：正文写正常行为；保证强度、前提与限制集中在「保证与限制」一节。实现与验证状态只写在 `docs/architecture/review.md`，其他文档链接它。
- **单一权威位置**：每条规则只在所属文档定义，其他位置链接。字段以 `contracts/schemas/` 为准，正文只列理解行为所需的字段。
- **图表**：新增图用 Markdown 内嵌 Mermaid，每张图回答一个问题，图前一句说明视角和线条含义；文字补充图中没有的理由与例外。字段、接口和选项比较用表格。
- **设计语义**：改写表述时保持已确认的设计；发现矛盾、缺口或过度设计，列入问题清单交用户决定。
- **交付检查**：`docs/architecture/validation/check_documents.py` 与各 `validate*.py` 通过；再由不带会话上下文的读者复述主线，读不懂的地方回改。

## Agent skills

### Issue tracker

Issues and specs live as Markdown files under `.scratch/<feature>/`. See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical triage roles use their default strings: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, and `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

This is a single-context repo using root `CONTEXT.md` and `docs/adr/`. See `docs/agents/domain.md`.
