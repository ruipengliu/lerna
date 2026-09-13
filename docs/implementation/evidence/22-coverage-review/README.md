# 已保存协议答案的覆盖评审

本目录记录实施助手独立阅读 `../22-frozen-snapshots/` 四份已保存答案后的覆盖判断，没有重新生成答案，没有将期望传入候选模型。它不是外部盲审或真实模型效果报告。每个 Review 绑定原文件 Record.Answer 的精确 JSON 字节摘要与冻结期望摘要；空白变化也会使绑定失效，不能跨输出复用判断。

| 用例 | 必要事实覆盖 | 含义 |
| --- | --- | --- |
| answerable | 0/2 | 答案和主张均为占位语句；引用内存在日期和长度，不等同答案陈述了这两个事实。 |
| conflicting | 2/2 | 两项主张分别保留 register 的 2001 与 archive 的 2003，引用对应所取得记录；范围保留未消解冲突。 |
| insufficient | 0/0，不适用 | 本例没有预登记肯定事实，仍须另外评审成本缺口和范围。 |
| fetch_failed | 0/0，不适用 | 本例没有预登记肯定事实，仍须另外评审拒绝原因与未核实承载力的披露。 |

`CoverageReviewComplete` 仅表示覆盖表所有必要事实已填判断，绝不表示答案合格、引用支持审查完成或整票通过。接口只验证绑定和计数，不验证评审者判断是否正确、是否独立。引用支持、范围、缺口与禁用结论仍是独立评审事项，没有用覆盖率取代它们。

复现绑定与汇总检查（从仓库根目录运行，不产生模型或网络请求）：

```sh
cp docs/implementation/evidence/22-coverage-review/check.go.txt /tmp/lerna-22-coverage-check.go
go run /tmp/lerna-22-coverage-check.go
rm /tmp/lerna-22-coverage-check.go
```

实际输出为 answerable 0/2、conflicting 2/2、insufficient 0/0、fetch_failed 0/0，unreviewed 均为 0。检查程序只重新核对已经保存的判断，不按用例名称自动评分。
