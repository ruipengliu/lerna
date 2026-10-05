# 04票03：冻结1.1下“未派发”观察的最小修正

**采用：修正验收oracle，不改1.1合同或Decision查询语义。** 原handoff §5把Decision/Command都写为not_found是不准确的；Decision.Get的result_unavailable只能辅助观察。必须以原Command当前授权not_found、fixture准确耐久无派发事实和真实Component入口观察共同验证。

固定integration：`eae357c996a40f601604c39d75ecda9684f1651e`。读取票03部分源码：`9bb61d42c98b5b8b2557a6949e49b30b767bd74d`，WT `/tmp/lerna-worktrees/content-snapshots-03`；这是当前局部接法依据，不是整票源码正确性/测试资格。仅只读，无native/DB测试、无另一审查轴输入。

## 1. 准确合同事实

- `contract/schema/1.1.0/values.json` 的 `$defs.DecisionGetResponse`只允许found、result_unavailable、unavailable、rejected；不存在DecisionGetResponseNotFound。`$defs.CommandGetResponse`实际包含not_found。
- `components/decision_engine/service.go:332` 的Get在当前读权限、原AcceptBefore和锁后时钟检查后，于384–386行把nil record返回result_unavailable；存储故障映为unavailable。不能给它增加not_found、改生成代码/golden，或把result_unavailable重新定义成通用不存在证明。
- `components/decision_engine/query.go:14–28` 的实际commandReader对缺原Command记录返回not_found；49–50行GetCommand走已有授权/owner解析入口。验收必须调用这个完整入口，不绕过授权直接读reader或SQL。
- adopted handoff §5第5项及紧随其后的“Component not_found”文字需窄改；票03 AC5要求编译overflow不接纳，AC7要求真实规则Component正常有/溢出无请求，均保持。spec与final-api-handoff的完整输入/各类溢出、真实正常对照亦不减项。

## 2. 每类overflow采用的联合oracle

1. 为该case固定独立owner scope内的原input revision、Snapshot/Decision/Command身份及原有限期限。只安装独立、当前、有限的get/command.get reader权限；不得为查询而预建Decision、binding、成功Snapshot或接纳receipt。9bb的ContextWorld已独立SeedControlAccess，可沿实际入口使用。
2. 从Compile调用之前开始观察真实规则Component入口。Compile必须返回准确context_overflow；受信fixture Observe同原input/revision显示overflow，准确 **binding不存在、dispatch不存在、attempts=0、无receipt/成功Bundle**。查询失败或过期不能当不存在。
3. 在有限实际dispatch推进后，经当前获准 `Command.get` 查询该原Command key，严格断言not_found。不要把rejected、gone、unavailable或任一Go error归为not_found；任何found（包括拒绝receipt）均不满足“根本没有请求”的本case结果。
4. `Decision.Get`可断言当前合法响应result_unavailable作交叉观察，但报告必须写其真实名称；它单独不证明未发请求、未创建任何事实或物理调用次数为零。
5. 实际重开fixture/Component owners，继续有限推进并重复fixture/原Command观察；覆盖的实际Decide入口到达仍为零。窗口须覆盖编译、已授权调度与恢复，且正常退出/原期限已闭合；有活动或未确认sender时不能从“暂时没看到”推断终身零请求。不要靠sleep或Step返回false单独验收。
6. 每类可容纳正常对照走同一装配/观察器，实际Decide收到原合法bytes/subject并由真实Service接纳，后续真实规则完成、准确artifact回读。证明观察位置接通；测试不伪造Component响应或将内部mock计数当出口事实。

Command not_found只证明当前原key无持久记录，不排除解码前拒绝或未提交请求；fixture attempts=0只是持久准入事实，不是物理入口计数。三者的资格不能互相替代。模型未装配，以上只证明本真实规则Component边界，不宣称Provider/模型调用证据。

## 3. 9bb上的最小真实观察接法

`conformance/internal/decisionfixture/context_dispatch.go:ContextDispatcher.Step`目前参数为具体 `*engine.Service`，实际只调用 `GetCommand` 和 `Decide`。推荐在**该fixture消费者处**定义只有这两种既有签名的小接口，实际 `*engine.Service`自然实现；正常运行仍直接注入Service。无需生产组件新增observer方法、业务registry或1.2 Decision协议。

验收注入完全转发的边界观察器：在调用真实Service.Decide之前同步记录入口到达（含不可解码请求）、原raw副本/摘要、可信subject与可解码的准确owner/Command/Decision身份；随后原样转发并保留真实响应/error，不替代授权、不预判overflow、不返回假receipt。GetCommand原样转发，查询不计为Decide。证据容量按本fixture实际有限attempt上界固定；超限使测试明确失败，不能丢记录后得到零。并发时同步保护；所有实际dispatcher装配都经过该边界，不能一条路径绕过。

这是一处实际组件公开方法入口的机械见证，**不是内部算法调用次数测试**。可由测试host保留跨owner重开前后的观察记录，重开Service后重新包裹并沿用同一有限观察窗口；不需要为此建持久业务表。它只证明本测试覆盖窗口与准确进程装配，不扩称网络发送、跨进程全局探针或SIGKILL后完整日志。若另做进程测试则需要另有外部耐久边界观察，不能复用内存零值冒充。

现 `ContextDispatcher.Observe` 用binding与dispatch的INNER JOIN；空JOIN不能分别证明二者都不存在。最小必要修正是扩展**现有可信fixture观察**，在同owner短Tx内按准确input/revision/Snapshot身份分别核binding和dispatch存在性（或等价的正确LEFT JOIN/存在性查询），返回明确presence字段；孤立/不一致事实不能映成正常“无派发”。保留锁后当前可信权限，允许合法overflow输入记录本身存在；不返回私表布局、不让测试直接SQL断言。仅观察形状变化，不需新增SQL表/迁移或公开wire字段。

## 4. 应替换的handoff文字与范围

§5第5项推荐替换为：“返回context_overflow；受信fixture准确观察无成功Snapshot binding、无dispatch责任、attempts0；当前授权Command.get查询原key为not_found；实际规则Component入口观察在编译及有限恢复窗口内无Decide到达，正常对照真实接纳并完成。Decision.Get按冻结1.1返回result_unavailable，仅辅助，不称not_found。仅内部mock或Step==false不足。”

其后通用句改成：“Command not_found必须由当前获准reader观察，forbidden/unavailable不能冒充不存在；Decision.Get沿冻结合同查询。独立有限reader权限不预建Decision/成功Snapshot。”其余完整强制上下文、每类独立overflow黄金/正常对照、原身份恢复和真实来源处理要求不变。

上述为已授权的具体默认决定。实现者仍需真实正常/overflow/重开观察验证；本决定没有执行注入或证明当前部分源码已通过七AC，也不承担总体03架构审查。
