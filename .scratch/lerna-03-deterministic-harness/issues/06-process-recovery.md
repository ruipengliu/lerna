# 06: 规则组件与目标的进程接替

**What to build:** 进程在发布或提交边界退出后，接替者查询原 Decision 和目标事实并继续原故障计划，不创造新身份或重复已完成步骤。

**Blocked by:** 01 原 Decision 的耐久规则提案；05 迟到效果与耐久故障计划

**Status:** claimed

- [ ] 复用02实际子进程／同步设施，在正常Decision输出发布与本地completed提交边界受控SIGKILL；独立连接公开get恢复原Proposal、产物、身份与固定回执。
- [ ] 跨owner发布已完成但Decision引用尚未提交的退出，用原发布key和摘要恢复，原引用读回一致，不造第二产物或假称跨owner原子提交。
- [ ] 独立目标在COMMIT前及提交后／响应前受控终止，普通query／read加observer区分无效果与已写响应未知；真实存储与子进程正常控制均运行。
- [ ] 故障协调者和目标重启保持scenario／event／cursor，同一持久scenario不重复已完成步骤；另一个隔离scenario同seed有限重演的定义明确。
- [ ] 所有子进程、管道、连接和临时数据有限清理；同步点来自真实阶段，不用sleep猜测，记录准确代码／DB／计划版本、输入和结果。
- [ ] 本票只验证正常Decision及target的进程恢复，不隐含依赖所有候选／取消或承担整片关闭；仅证明进程SIGKILL，不证明断电、供应商幂等、生产容量或可用区耐久。

## 最终接法注记（whole02已退出）

最终依据 `../final-handoff.md`（代码554874470d5abeb71fa743708580f3121b8944f1，退出文档df2dbe5624120bc258dc7419ea022a08ebc0d6b0）。票10 holder历史失败阻断Cleanup的P2已修复并通过准确证据：错误保留聚合，退出确认后继续安全清理，退出未知则保留scope。ownedFixture仍是demo waitStore/Host/child，不能冒充Decision/source/publisher/target装配；实际第二consumer需要时仅共享窄scope/admin与有限进程机制，保留强类型writer与专有fault/history，不建通用registry或补假demo接口。既有未知scope/CID限制保留。本票仍仅依01+05、6条AC，不承担02/03或root全片final-close。


## Comments

2026-10-03，root依据授权Astra批准的粒度/真实edges及最终df2dbe5前置退出发布；本票验收尚未实现。等待上述直接前置resolved，不能以spec ready替代实现依赖。

2026-10-04，root正式claim本票。直接前置01已resolved并merge5307702（最终源696ac49、交付d806a92）；05已resolved并merge077f616。准确push c9de1ba的CI37174778053全部success，见[CI记录](../ci-verification.md)。按用户已授权的全部实现任务及已采用技术决定，在新的独立worktree/branch实施；原01工作树保留，不能修改归档或用候选pin冒充前置。验收seams已在整片授权固定为公开Component／真实fixture Source与target普通接口及独立observer，物理生命周期仅使用明确标注的机械driver观察。所有build/DB/全套测试须先获root独占槽；本票仅关闭自己的AC，不承担whole03或后续依赖。
本票实际接法见[正式采用记录](../process-lifecycle-handoff.md)与[ticket01 API](../ticket-01-api-handoff.md)。

2026-10-04，独立06分支已逐条完成实际生命周期及进程边界 vertical，仍 claimed、六AC尚未关闭。机械 nativeClose/并发有限等待、Observer首次结果、构造 partial holder、file/parent FD 首次结果、Start handle发布窗口及Stop-before-Start门禁均有实际可运行 red→green；这些不持有物理DB scope，也不声称原生SQLite/PG/file故障。共享有限 Child/Inherited 已由真实02 demo与真实06目标/Decision两个消费者调用，旧SQLite正常回执、领取完成及阻塞管道截止回归 normal3.556/race8.675均exit0；提取期间缺失函数及多余import的编译错误保留为作者失败历史，不称behavioralred。

目标真实COMMIT前已有normal0.088/race3.174；提交后／回复前真实red0.066→normal0.101/race4.204，固定代码43b4b1684ee00c9f415a73cf19b0784f7c6fc498。SIGKILL由实际WaitStatus和独立reply EOF确认，重开普通Query/Read及Observer区分已知回滚无处理事实与已提交回复未知，新代协调者重读同scenario/event/cursor后保持原bytes/version/window/receive/DatabaseID。后批作者漏传外部scope registry：六个实际路径均在Mkdir后、开库前本地write+fsync并逐一absent，外部账本仍原七条，未伪称十三条登记。

双owner Decision正常子进程 control normal0.688/race4.684；Source Publish成功后／Decision Finish前真实red1.159→normal2.025/race6.401，代码a29b54c95533f4bae7eda28f2e279fe2a382e5a8；实际SaveDecision completed+原callback Complete成功后／CoreCOMMIT前 red1.029→normal1.959/race6.206，代码e0701e8a413a7b1818ac894331fba2039109add1；nativeStore.Within实际返回nil后／reply前 red1.107→normal1.152/race5.552，代码90933274e5a054c3eb42fc7b3c84153ce62c970e。独立公开Get及授权Publisher读取保留原Proposal/ref/body、固定receipt与故障前公开usage（含原下界/MeasurementsComplete），不假称跨owner原子提交。PG18.6、sync on/read committed、实际迁移版本校验和已记录，40个确切已ack/fsync PG scope独立IN-set查询均absent。所有批次结束均实际Wait／poll session退出后释放root独占槽。待补目标pending/late进程恢复、隔离同seed定义、自身剩余生命周期控制、最终root合流／受影响回归及实际双轴评审；不承诺whole03关闭。

2026-10-04，pending/late 真实正常与SIGKILL恢复 normal0.139/race4.201（232973e），pre-native drain 可重试正常0.025/race1.036，实际FD三调用路径正常0.082/race1.133（2306e69）。两个独立scenario／DB同Seed73、明确保存正常与DropResponse两steps重演 normal0.102/race5.182，各代实际zeroWait；原完成event／cursor不重做。计划响应丢失未称SIGKILL。外部target账本23确切路径、旧local-only六路径分别absent；本批无PG。六AC实现／事实出口／历史失败／清理界限见[实际映射](../ticket-06-api-handoff.md)。仍claimed，待最终受影响回归／基础检查和真实双轴评审；不纳入尚未resolved的02／03或wholeclose。
