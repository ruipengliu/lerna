# 术语统一检查记录

范围和语义结论见[方案审查第 25 节](../../docs/architecture/review.md#terminology-review)。本目录保存本轮实际执行的静态检查与图形检查；没有真实服务、数据库故障或性能实验。

- 文档结构和链接：[check_documents.log](check_documents.log)。
- 静态契约：[完成投影](validate.log)、[协议序列](validate_protocol.log)、[传输与签名](validate_transport.log)、[Brain](validate_brain.log)、[离线租约](validate_lease.log)、[发布恢复](validate_release_recovery.log)。
- 机器结构、图形结构及最终摘要：[structure-checks.json](structure-checks.json)。机器可执行结构仅修改 effect_class 枚举名称；原生图和 HTML 图册仅修改文字及文字属性。
- Mermaid：[最终图源与渲染清单](render/manifest.json)、[前 22 张目视记录](mermaid-visual-review-a.json)、[后 22 张目视记录](mermaid-visual-review-b.json)。44 张全部实际渲染，发现的问题修复后重新查看。
- 原生图：[15 页渲染清单](drawio-manifest.json)、[最初 14 页逐图记录](native-visual-review.json)、[新增第 20 页及其他材料检查](root-visual-review.json)。图册共 21 页，其中 15 页文字发生变化；节点、连线、字段及几何布局不变。
- HTML 图册：用浏览器检查全景、编排器、执行、记忆、评测、扩展等代表页面，并展开评测及扩展的关系表；截图在 [output/playwright](../../output/playwright/)。使用本机回退字体检查，外部字体加载失败与 favicon 缺失未影响可见文字；未声称验证所有浏览器或字体组合。
- 概念图片：使用现有 PNG 作为输入，以 [image-prompt.txt](image-prompt.txt) 更新文字并目视核对；最终文件为 [design-concepts.png](../../docs/architecture/diagrams/design-concepts.png)，保留原布局与主要关系。

保留原 PlanMaterializer、typed_decision、TaskGate、StartBarrier 等类型名；中文定义说明项目含义。原标题被引用时保留显式锚点。旧术语可出现在历史记录或旧锚点中，不代表现行正文继续采用。
