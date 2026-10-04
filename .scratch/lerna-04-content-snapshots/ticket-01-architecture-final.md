# 04 ticket 01 — 14ead 固定源码 F1 follow-up

2026-10-04。新固定源码 **`14ead831b8834892da5ff13a9b883494247f327f`**，比较旧 delivery `72da5ee820a69695553ff208e10a8ba31804d0b8`（产品源码对应 `46d6ca26e4c2a4e9cf6db95e890b641f6bf2aa97`）。本次只读 immutable objects，未运行 native/build/test/DB，未读两轴报告或 moving tree。

**结论：F1 的准确授权优先顺序已在源码关闭；无需新增框架或必要架构重构。** 旧46/72报告及当时 open finding 保留，本稿记录后来的真实源码修正，不将历史发现倒改为当时已通过。

## 差量及资格

实际 git diff 为 **2路径、214 additions / 1 deletion**：`domain/content/service.go` 和新增 `conformance/component/content_target_policy_test.go`。除此之外所有 tree 对象相同。因此既有 A/B/A1/A2/B1、direct-source/association/replay、五消费者 ownership 的源码闭合结论未被此次修改影响。

Repository/Record/FixturePolicy/Objects 的定义、PG政策适配器、0001 migration、身份/tuple算法及外部1.2形状均未改。此次不是新接口或授权框架。

## F1 source closed

`domain/content/service.go:338–355` 为 read/disclose 分别收集完整 policy.Ref；`:356–374` 取得真实版本后保留可信 DB Now、初始 admission 的 AcceptBefore 与当前 readBefore 检查。

`:375–387` 决定准确授权对象：存在 record 时用实际 record.Ref，否则用 request.Ref；两项 policy.Ref 都须等于它，失败 forbidden 且清除 record。**此步骤位于 not_found (`:388`) 和 request/record mismatch→integrity (`:392`) 之前。**

由此保持已采用的全部顺序：

| Policy / request / actual record | 观察 |
| --- | --- |
| A / A / A | 继续当前状态与正文门禁 |
| A / B / A | 已获准观察实际 A，错误请求声明为 integrity |
| B / A、B或C / A | 错误完整资源许可，forbidden，不先透露存在或不匹配 |
| A / A / absent | 精确获准查询可 not_found |
| B / A / absent | forbidden，不能借同版本身份的别种声明观察缺失 |

同一 observe 在 `:462` 的读前短 Tx 和 `:486` 的返回前短 Tx 实际复用，所以没有只修其中一个门禁。对象 I/O 仍在两者之间；readBefore 保留更严上限；最终 gate 的 admission=false 不把 AcceptBefore 改为 I/O 完成期限。查询仍不保存回执/Job或更改出版历史，不要求物理收回已经披露字节。

## 新测试源码与小注释更正

新增测试 `TestContentTargetReadPoliciesBindExactPublishedDeclaration` 遍历 hash/media/length × before-read/after-native-read 六场景：正确政策先有正常读回和错误请求 integrity 对照，改变受信完整 policy 后禁止正文；实际独立文件字节/原 receipt/历史 published 不变，恢复准确政策后可读。after-native-read 使用既有有限同步门控制实际完整回读后的顺序，不冒称 native 存储故障。

`TestContentTargetAuthorizationPrecedesExistenceAndMismatchObservation` 覆盖 wrong policy 与第三种请求、wrong policy 与其自身请求、absent 三种顺序；各有准确政策下的 integrity/not_found 或正文正常对照。测试借公共 Content/Command、受信政策与独立文件观察，不用私表业务 oracle。

**一处非行为注释应随文档收尾纠正：** `content_target_policy_test.go:44–45` 仍说 policy full-ref “evaluated after ... exact-record comparison”，这是修正前的次序描述；实际源码和测试下半部已正确为“先匹配实际 record 的完整政策，再裁决请求 mismatch”。改这条注释不改变 F1 source closed，不要求为文字重跑 native。

## 执行记录与仍待事项

root报告六场景 red54796→green55134，顺序 red16987→green44631，race47356/22.361s 为实际 native0且有正常对照；本代理只核上述固定测试源码，**未独立读取原执行日志或重跑**。本次审查时 locked check46700仍进行、交付文档及资源audit仍待。不能据源码闭合推出所有最新检查完成、首票8AC已接受、合入/推送/CI或04 whole退出。

## 票02条件 handoff 的适用性

`/tmp/lerna-04-ticket-02-conditional-handoff.md` 基于46/72的 Record/Repository/Policy/Job/SQL 接法在14ead仍适用：这些实际对象字节未改。其“F1仍open”是写作当时状态，现由本稿更新为 **14ead源码关闭、执行/首票正式接受仍分开**。原文保留历史，不假改成当时已完成。

票02仍硬等01最终接受合入和准确最终 SHA/API 复核；本稿不claim、不实施票02，不把条件准备当已有闭包/传播/holder能力。四处 generator native-result 归并仍维持此前 Worth/当前保留的决定，本次没有理由重开。
