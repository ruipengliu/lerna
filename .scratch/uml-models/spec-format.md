# 内容规格格式

每个被分配模块输出独立 `<slug>.json`，仅包含如下结构；绘图由主 agent 统一完成。ID 在本模块内唯一即可。

```json
{
  "module": "brain",
  "title": "大脑",
  "component": {
    "nodes": [
      {"id":"api","name":"Brain API","kind":"interface","boundary":"provided","methods":["brain.decide","brain.get"],"source":"brain/README.md"},
      {"id":"facade","name":"DecisionService","kind":"component","boundary":"internal","source":"brain/implementation.md"}
    ],
    "edges":[{"from":"facade","to":"api","type":"realization","label":"提供"}],
    "notes":["简短的当前机制条件；不是新要求"]
  },
  "data": {
    "nodes":[
      {"id":"decision","name":"DecisionRecord","owner":"Brain","schema":"DecisionRecord","attributes":[{"name":"decision_id","type":"ID"}],"source":"brain/README.md"}
    ],
    "edges":[{"from":"a","to":"b","type":"association","fromMultiplicity":"1","toMultiplicity":"0..*","label":"关联含义","source":"brain/implementation.md#data-flow","constraint":"可选的适用条件"}],
    "notes":["独立状态或清理边界"]
  },
  "reading_notes":["导读应说明的语义及边界"],
  "sources":["brain/README.md","brain/implementation.md"]
}
```

- 每模块组件页建议 4–6 个内部组件＋1 个提供接口＋最多 2 个依赖接口，合计不超过 9 节点、12 边。可以依原文将共用职责合成一个框，但保留真实名称；边只选关键依赖。
- 领域页 5–7 类、最多 8 条关联、每类 3–5 个关键属性。不要把全部 `$defs` 变成类，不编造继承或方法。引用没有销毁语义，使用普通关联；每条关联两端均标真实多重性。
- interface.methods 必须是登记在 methods.json 的精确方法。内部非线格式 port 用 `methods:[]` 并注明来源和边界，不能伪造公共方法。
- schema 可为 null，表示有正文依据的内部记录；记录属性必须能在 source 找到。Schema 属性使用其精确名称，可简写类型（ID、Revision、Ref、Enum、Set 等），不得改义。
- 跨 owner 的关联须在 owner／notes 中明确。局部数组个数不能证明反向数量；若全局反向不受限用 0..*，有适用条件写 constraint。不能证明的关系可不画，不能编造。
- notes 每页 2–3 条短句，长解释放 reading_notes；当前实现类尚未存在。
