# 切片01架构候选：合同测试runner生命周期定案

2026-10-03。依据 `/tmp/lerna-01-architecture.json`、`/tmp/lerna-01-architecture-exploration.md`、`/tmp/lerna-01-runner-measurements.json`及固定HEADb825c2d的test-contract/Go与TS runner。用户已授权代为决定，不再要求人类选择或quiz。本决定不新增ADR，不改产品接口。

## 选择：当前实施，但与正确性修复分开

接受唯一Worth exploring候选，优化共同合同harness的runner生命周期。158夹具/68正例使每语言启动226次进程；公开ID runner七次采样中位TS394.62ms、Go16.32ms。这里已有反复执行测试的实际成本，不是为未来供应商设计抽象。

92.87秒只是按样本外推的重复启动/ID成本，**不是全套实测耗时或承诺收益**。实施前需在正确性修复后的固定commit完整量一次当前test-contract，修改后同环境同输入再量一次，分别记录进程启动数、完整wall time、夹具/正例数和结果。

先完成已复现的Go紧凑编码与TS Schema不可变修复、跑其回归，再以该修复commit为基线开独立architecture ticket/worktree。可以复用同一个实施代理顺序处理，但必须独立commit/差异与前后测量；不把两类改动混为一个不易回退的fix pass。原因是持久进程复用Ajv必须先确保Schema不可变，否则优化会放大已知状态污染问题；同时新正确性fixture不得只出现在“后测”，造成不公平计时比较。

## 最小生命周期和私有接口

harness每次运行启动一个Go runner和一个TS runner，复用到本次测试结束；每个runner同时最多一个在途请求。采用私有有界逐行消息协议，不建通用RPC/worker池/并行调度平台，不新增产品包或公开接口。

建议私有帧：

```
request: {id:string, schema:string, wire_base64:string}
response: {id:string, ok:true, wire_base64:string}
        | {id:string, ok:false, error:{code:string}}
```

wire_base64承载**原始字节**；不要先把fixture.wire解析成对象再发送。这样重复键、数字词法、空白、literal转义、Unicode、非法UTF-8及精确正文长度仍然交给被测公开codec。IPC帧自己的JSON不是产品线协议，不受产品“所有数值用字符串”Schema解释；采用string id本身已足够简单。

父进程的私有生命周期只需start/run/close等实际操作。每个run产生唯一id，核对响应id、分支和有限长度。启动、读取、退出、超时、stderr诊断及cleanup集中在同一harness私有模块/文件，不把业务Schema验证移到父进程。

可在现有runner加batch模式并保留原单次CLI，用于启动测量及必须全新进程的回归；这属于测试工具接口，不是SDK公开API。若保留原CLI造成明显重复，以共用一次roundtrip函数组织，不能复制编解码实现。

## 保留的负载和验证边界

Go每条请求必须仍进入当前生成的typed run/Decode[T]/Encode；TS必须仍进入公共decode/encode或decodeCommand。保留每个正例两条真实往返：Go首编 -> TS解编，以及TS首编 -> Go解编；不得用父进程JSON.parse/对象比较替代第二语言实际调用。

父进程继续对独立原始期望值做准确比较。反例继续验证公开错误码，合法对照继续成功；一条schema_invalid等预期拒绝只是该case结果，runner继续处理后续case并最终正常退出。不能用某个runner进程非零退出作为普通业务拒绝，否则仍会把崩溃伪装为合法负例。

## 有限时间、崩溃与输出约束

- 保留原**每次公开runner调用10秒**上限，包含同一case的decode/encode；不是给整批一次10秒，也不是只设总批次超时。第一条可将启动纳入相同10秒，或另设明确有限启动期限，均须记录。
- 父进程等待该id结果使用实际timer；超时立即把本次测试标为基础设施失败、终止对应runner并取消剩余流程，不静默重启/重放该case后返回绿色。保留fixture名/schema/lang/direction/id供复现。
- 非预期进程退出、signal、缺响应、错id、非法IPC、stdout污染、超限帧均是harness失败，不映射为schema_invalid来“通过”负例。
- IPC帧与stderr都有显式有限内存上限；建议帧8MiB、stderr64KiB。帧上限必须容纳现有所有原始超限负例及base64膨胀，超出则明确harness失败，不截断wire后继续测试。产品公开1MiB上限不改变。
- Go bufio.Scanner默认64KiB不够，必须明确提高至私有有限帧上限或用有界reader。TS组装帧也必须有界，不能等任意大行读取完成才检查。
- stdin关闭、成功、断言失败、timeout和Ctrl-C都要清理两个child和临时Go binary；等待退出有有限期限，必要时kill。及时清理timer/listener、观察迟到Promise rejection；不遗留持续运行的后台进程。

## 隔离决策与可接受变化

持续进程会替代原来的“每例新进程”隔离，这是**显式接受的测试实现变化**。理由是当前产品codec只保留只读Schema/编译缓存，不保存调用方业务状态；返回值必须独立，任何跨case数据泄漏都属于bug，不能依赖重启把它藏起来。

每条请求使用全新的输入字节及返回对象，编译缓存可以复用，但不得保留或修改前一case的payload/ref/subject。已修复的deep-frozen Schema是前置。增加有意义的隔离证据：同进程坏例后紧跟正常例仍成功；同一合法值重复执行一致；至少对共享fixture反向顺序运行一次并保持结果，证明初始化先后不影响接受集合。

保留需要新进程才能复现的测试（例如首次Ajv编译之前尝试改Schema）作为独立进程回归，不因batch优化删除它们。不是每个fixture都必须新进程，但每个原有故障语义和首次初始化风险必须继续可观察。

## 验证与退出

复用全部现有共同fixtures、准确期望值、真实跨语言往返、digest/query/negotiation套件及正确性修复新增回归。只为新生命周期规则增加必要的小型故障探针：预期拒绝后继续、超时、崩溃、非法/错id响应、退出清理。它们测试新的可观察有限失败行为，不是镜像私有实现。

正确性结果必须完全保持；基线和修改后都记录完整wall time和实际启动数。只有实测收益才报告更快。若生命周期复杂度意外扩大或完整耗时无明显改善，允许回退本独立架构commit而保留正确性修复；不能为兑现外推数字牺牲逐例上限或双语言证据。

不优化其他当前模块，不给runtime/query/digest建立通用框架，不调整本版公开方法清单。架构票完成后主代理仍完成统一复审及切片退出记录。
