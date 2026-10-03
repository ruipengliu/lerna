# Agent Memory 开源实现：关键结论

来源核对日期：2026-09-28。建议保留本项目 Memory owner、来源关系、版本化 Content、当前授权及清理责任，借鉴外部库的算法与内部组织。没有运行这些库或复现性能；接入规则见[现行 Memory](../architecture/memory/README.md)。

| 实现 | 可以借鉴 | 必须保留的适用边界与来源 |
| --- | --- | --- |
| Mem0 OSS | 结构化事实提取、去重、更新和可选重排 | BM25 在语义候选上加分，不是两路全集合并；delete 仍可保留历史。[搜索][m0-search]、[删除][m0-delete]；固定版本为 Apache-2.0，[许可证][m0-license] |
| Graphiti | 事实有效时间、失效时间和系统记录时间分开；混合检索配方 | 图数据库引入新的同步与清理责任，不能直接替换 PG 权威记录。[时间字段][g-edges]、[检索][g-recipes]；[Apache-2.0][g-license] |
| Letta Code | 后台 worker 在私有 worktree 准备记忆修改并处理合并冲突 | Agent harness 的职责范围大于 Memory；Git 保留版本不等于物理删除。[worker][l-worker]；[Apache-2.0][l-license] |
| LangMem / LangGraph Store | 纯提取 manager 返回候选，适合“模型建议、owner 决定”的边界 | 带 Store 的 manager 还会直接 put/delete；TTL 可配置为读取刷新，不能代替固定保留期限。[manager][lm-manager]、[Store][lg-store]；[LangMem MIT][lm-license]、[Store MIT][lg-license] |
| MemOS | 树状记忆、图扩展、全文/向量检索与重排 | Python 服务与本地 TS/SQLite 插件是不同流水线；API 名称不能证明统一事务或完整清理。[Tree][mos-tree]、[本地检索][mos-local]；[Apache-2.0][mos-license] |
| memU | 外部生成候选，存储端增量向量化和检索，合成与存储解耦 | commit_results 的 repository 调用各自提交，存储失败可部分成功；progressive_retrieve 不自动意味着多轮充分性判断。[提交][mu-commit]、[检索][mu-retrieve]；[Apache-2.0][mu-license] |
| OpenViking | L0/L1/L2 分层上下文、检索轨迹和经验血缘 | ACL 默认关闭，启用后随索引更新且不保证强一致；session diff 可能保留已删除内容。[检索][ov-retrieval]、[ACL][ov-acl]、[session][ov-session]；主项目 [AGPL-3.0][ov-license] |

## 接入时先验证什么

1. 写入以独立保存许可和原命令为边界，候选不直接成为权威记忆；纠正、过期和矛盾记录有准确版本。
2. 索引只产生候选，返回与采用时仍检查当前授权和来源；关闭、派生清理与物理残留分别跟踪。
3. 用相同任务比较字面/BM25、获准的混合检索与按需整理；统计构建、更新、在线调用和清理全成本。
4. 验证跨租户、重复写、部分失败、重启恢复、摘要残留和撤权传播，不能只测试“能检索到”。

上表链接已固定源码版本。后续确需复核或移植时，再按[按需下载说明](README.md#source-download)取得相关仓库；许可证结论仅对应所引版本。论文依据见[记忆研究结论](agent-memory-papers-2026-09-28.md)。

[g-edges]: https://github.com/getzep/graphiti/blob/6b4b56ff6f4b1e4e69c3c3c5487cf1b8762c483a/graphiti_core/edges.py#L260-L285
[g-license]: https://github.com/getzep/graphiti/blob/6b4b56ff6f4b1e4e69c3c3c5487cf1b8762c483a/LICENSE
[g-recipes]: https://github.com/getzep/graphiti/blob/6b4b56ff6f4b1e4e69c3c3c5487cf1b8762c483a/graphiti_core/search/search_config_recipes.py
[l-license]: https://github.com/letta-ai/letta-code/blob/c864f1532b328aab4bb76cc68a86d5f014de27b8/LICENSE
[l-worker]: https://github.com/letta-ai/letta-code/blob/c864f1532b328aab4bb76cc68a86d5f014de27b8/src/agent/subagents/memory-worker.ts#L69-L215
[lg-license]: https://github.com/langchain-ai/langgraph/blob/07b33185eab893be2ed031eedae52f09314bf77c/LICENSE
[lg-store]: https://github.com/langchain-ai/langgraph/blob/07b33185eab893be2ed031eedae52f09314bf77c/libs/checkpoint/langgraph/store/base/__init__.py
[lm-license]: https://github.com/langchain-ai/langmem/blob/9d033b47d9ce53e37e92c92241b0496c0278932e/LICENSE
[lm-manager]: https://github.com/langchain-ai/langmem/blob/9d033b47d9ce53e37e92c92241b0496c0278932e/src/langmem/knowledge/extraction.py#L536-L692
[m0-delete]: https://github.com/mem0ai/mem0/blob/94c3fe9f238f3dbf29c9ce98643bd71eb13077cd/mem0/memory/main.py#L2114-L2142
[m0-license]: https://github.com/mem0ai/mem0/blob/94c3fe9f238f3dbf29c9ce98643bd71eb13077cd/LICENSE
[m0-search]: https://github.com/mem0ai/mem0/blob/94c3fe9f238f3dbf29c9ce98643bd71eb13077cd/mem0/memory/main.py#L1642-L1701
[mos-license]: https://github.com/MemTensor/MemOS/blob/a7367d07e55db61099f7b4e2c1108bc5831a24f3/LICENSE
[mos-local]: https://github.com/MemTensor/MemOS/blob/a7367d07e55db61099f7b4e2c1108bc5831a24f3/apps/memos-local-plugin/core/retrieval/retrieve.ts#L291-L542
[mos-tree]: https://github.com/MemTensor/MemOS/blob/a7367d07e55db61099f7b4e2c1108bc5831a24f3/src/memos/memories/textual/tree.py#L55-L210
[mu-commit]: https://github.com/NevaMind-AI/memU/blob/2c050bc9681a4c0aff1af211a000e73d14f33356/src/memu/app/agentic.py#L343-L391
[mu-license]: https://github.com/NevaMind-AI/memU/blob/2c050bc9681a4c0aff1af211a000e73d14f33356/LICENSE.txt
[mu-retrieve]: https://github.com/NevaMind-AI/memU/blob/2c050bc9681a4c0aff1af211a000e73d14f33356/src/memu/app/agentic.py#L187-L329
[ov-acl]: https://github.com/volcengine/OpenViking/blob/b2f1f9d2f12cd031ec2f90b069b9b78528c5f2da/docs/en/concepts/15-acl.md
[ov-license]: https://github.com/volcengine/OpenViking/blob/b2f1f9d2f12cd031ec2f90b069b9b78528c5f2da/LICENSE
[ov-retrieval]: https://github.com/volcengine/OpenViking/blob/b2f1f9d2f12cd031ec2f90b069b9b78528c5f2da/docs/en/concepts/07-retrieval.md
[ov-session]: https://github.com/volcengine/OpenViking/blob/b2f1f9d2f12cd031ec2f90b069b9b78528c5f2da/docs/en/concepts/08-session.md
