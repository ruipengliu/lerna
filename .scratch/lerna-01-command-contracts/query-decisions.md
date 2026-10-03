# Ticket04：回执提示、传输结果及只读事实接口

2026-10-03。检查工作树 `/tmp/lerna-worktrees/contract-04`；本决定细化ticket04，后续ticket05负责受信身份入口。不改代码，不需ADR。

## 闭合分支与类型表达

Schema使用命名的闭合oneOf分支，各分支通过state/status/kind唯一判别。TS生成真实判别联合；Go可用私有分支封装配New/As构造与读取函数。封装须防止外部制造多分支同时存在的非法值，编解码仍执行机器合同验证。无效外部输入返回可判断错误，不能panic。

## 固定回执next_action

采用最小提示：accepted/applied允许可选 `next_action:"query_original"`；rejected允许可选 `next_action:"resolve_rejection"`。首版不增加no_action；字段省略已经表达“未提供下一步提示”，能避免no_action被读成异步责任已关闭。字段若存在，必须属于该state允许的唯一值，不能写null或空字符串。

提示是原决定时提供的固定信息，和回执一起保存；进展从active到succeeded/failed时不能改写它。调用方以当前progress作为当前事实，不把旧提示当作当前状态。

- query_original：沿固定原CommandRef查询原命令或其已知关联对象；不授权新建同义工作、换owner、修改原payload或延長原accept_before。
- resolve_rejection：读取拒绝依据，由有权调用方重新决策；必要条件变化且确实需要新的业务意图时，创建明确的新command_id。不得复用原ID提交修改后的内容；该提示本身不授予权限、不保证新请求会成功。

不存在“accepted等于Task成功”或“applied以后没有异步责任”的含义。

## TransportOutcome

只提供当前切片确实需要的两种表达：

```
{status:"received", receipt:CommandReceipt}
| {status:"commit_unknown", command_ref:CommandRef,
   next_action:"query_or_retransmit_original"}
```

received只表示收到了可验证的固定接纳回执，不表示Task成功。commit_unknown是传输不确定性，绝不是第四种CommandReceipt.state；它必须保留原CommandRef。query_or_retransmit_original只允许查询或重发完整原命令；重发保持原ID、owner、payload、expected_revision和accept_before。

不为尚未实现的网络栈预建所有发送前/发送中/认证/连接失败分支。首版TransportOutcome明确只覆盖“已有回执”与“未知是否提交”两种被建模情况，不宣称这是未来所有网络失败的穷尽分类。后续真实传输切片再通过准确版本定义需要的新分支。

command.get的unavailable表示当前无法取得可信读取结果。它与commit_unknown是不同维度；不可因为已有read unavailable便声称所有写传输不可用都已建模。真实网络不存在时，只展示类型及合同夹具，不声称已验证传输恢复。

## ReadCommandFacts事实读取接口

`ReadCommandFacts(ctx, raw, reader, clock)`可作为当前ticket04的低层只读事实测试接缝：解析已登记command.get、检查查询截止、固定原引用、读取注入事实、验证输出。ctx/有限期限贯穿，读取不能创建CommandReceipt/Job或启动模型、安装、动作。

该接缝没有真实认证，不是已完成的Application/Component受信查询端点。不要在SDK公共barrel里把它提前重导出为可直接使用的业务查询API。若Go因跨包装配需要导出符号，doc明确它是调用方已完成授权后使用的低层读取原语；目录可导入不代表稳定公开业务承诺。

ticket05必须增加受信认证入口，在事实读取前检查主体、tenant、owner/ref和精确读取权限；只有该入口进入最终支持清单的可调用业务路径。不能只把05身份参数加在文档里，而继续让示例经未鉴权seam访问所有事实。

clock用于本次查询请求accept_before；不得因原写命令accept_before已过便拒绝读取其原固定回执。查询自身关联command_id不建立新的持久业务责任。

## 验证范围

复用公共合同套件验证所有状态分支、字段条件和非法组合；查询facts源提供正常、进展变化、gone/not_found/unavailable和无副作用对照。固定回执与当前进展引用/revision关系采用 `/tmp/lerna-receipt-decisions.md`。无需添加对简单转发或私有调用次数的镜像测试。
