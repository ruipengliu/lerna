# Protobuf 实对象验证（H1）方法

| 日期 | 修订说明 |
| --- | --- |
| 2026-10-06 | 按文档规范拆分方法与结果；原报告及原始数据保存在实施报告，新增本地指标的真实公共采集要求。 |

上游为[核心契约 7.5 与 H1](../core/contracts/README.md#75-schema-技术首选-protobuf待实际验证)和[ADR 0004](../../adr/0004-language-and-stack.md)。本文规定怎样验证，不记录测试结果。[实施报告](../../implementation/m1/protobuf-h1.md)提供历史及当前实际证据；报告不改变通过标准。

## 1 采集和通过标准

使用生产共用的进程内装配，通过公共命令和查询取得真实对象。独立目标、模型提供方及原生文件边界记录实际调用、效果、每次发送费用；它们与原负责方持久事实共同构成判据。边界包装器必须委托生产执行与最终资格检查，不能模拟返回或从供应商 JSON 拼装核心状态。每个新增采集流程先以真实缺样本或字段断言失败，再接入统一采集。

每个当前样本必须完成生成 Go 绑定、二进制和 ProtoJSON 往返，保留精确引用、64位整数、可选费用缺失与零值以及原字段语义。二进制未知字段保留和语义入口的未知安全字段拒绝分别验证；结构解码成功不构成授权或业务资格。类型清单包含所有生成消息，未采样类型不得从分母隐藏；显式不支持、私有 whole-object 未开放和可选未实际行使分支分别说明，不用空对象补齐。

统一入口 `TestCaptureProtobufMainline` 保留全部既有场景名与真实生产流程；覆盖MODEL、API、FILE、准入/确认/预算、来源/派生/R7、取消/非成功关闭、核对/安全重发、默认有限driver及其原请求和回报。完整正文与结构投影分别读取、固定Result与晚到当前事实分别保存，不能从投影重建不可用正文。公开 `PendingGoal` 来自原Job载荷，不直读业务SQL。

即使流程在发送前取消，实际配置仍必须使用本版本支持的固定实现身份；不能为捕获授权或确认而把目标声明改成未实现的适配器，再要求恢复忽略它。发现旧采集fixture使用未支持身份时，保留失败及旧语料，修正新producer的首次公共配置；不得改写旧数据库或放宽全部历史预检。

本地指标的十一种消息从实际 `Harness.QueryMetrics`、Tasks/Ledger/Trace公开只读返回采集，包括其真实nested来源、时延桶、年龄桶和disabled诊断。核验原准入、UNKNOWN/计划分母、R7 source/accepted/indexed/ACK以及查询前后原业务事实和外部次数；NO_SAMPLES/MISSING/PARTIAL/DISABLED不能当健康0，核对时长缺原端点不造SLO。细则见[观测口径](../topics/observability.md#91-m1-本地指标口径)。

## 2 固定语料与覆盖

固定语料 `conformance/protobuf/testdata/mainline.json` 只含上述实际响应和受理命令。包含运行产生的UUID、时间、模拟地址及合成内容；测量不访问这些地址。重新采集不能要求随机字段逐字节相同，但必须保留原场景身份集合。覆盖报告分别列schema SHA、全部生成绑定、递归真实类型、实际出现字段、每个顶层对象binary/JSON尺寸及未采样范围；类型出现不代表全部字段、枚举或状态已验证。

覆盖和性能只能引用同一个固定语料和Schema。重采或新增契约前按原字节归档旧语料、报告、原始数据及来源版本；不得将旧数值标为新Schema结果。

## 3 复现与成本测量

在仓库根目录显式写入指定语料；普通测试不改固定文件：

```sh
LERNA_PROTOBUF_CAPTURE="$PWD/conformance/protobuf/testdata/mainline.json" go test -race ./conformance/admission -run '^TestCaptureProtobufMainline$' -count=1 -timeout 20m -v
go test -race ./conformance/protobuf -count=1
LERNA_PROTOBUF_BINARY_ARCHIVE=/Volumes/Data/proj/lerna-m1-context/ticket23-final-measurement-binaries scripts/measure-protobuf.sh /Volumes/Data/proj/lerna-m1-context/ticket23-final-measurement
```

测量窗口固定源树、生成器/运行库/平台与语料，协调其他工作停止测试、构建和bench。脚本先编译，归档实际最小程序源码、baseline/binding/measure三个二进制和SHA，再用该测量二进制输出清单并运行每个样本四项编解码三轮100ms；逐轮保留ns/op、B/op和allocs/op，不挑最好值。生产语义成本仅对实际 `command.ValidateGoal` 和 `command.ValidateHeader` 入口计时，不包括持久读取、跨记录事务或整个门禁，不能称通用对象语义验证器。

独立 `/usr/bin/time` 输出已编译测量进程最大RSS：Darwin单位为bytes，Linux为KiB；包含Go运行库、测试框架、样本和GC，排除编译器和SQLite。包体积分别报告proto/生成Go源码、相同trimpath/链接选项的最小baseline与binding程序及增量。RSS不等于累计分配，也不是业务宿主或手机准入资格。

ProtoJSON不是规范化字节；Go运行库可按二进制种子选择可选空格。不同binary的JSON尺寸可能相差结构逗号数的-1、0或+1倍；不能写死旧轮次系数或要求两份清单字节一致。Schema、类型、字段、sample names与binary尺寸必须对应一致；最终JSON尺寸及bench只取同一个归档测量binary的清单和结果，保留不同清单原件及逐样本解释。

必须保存环境、提交与完整当前源摘要、固定语料、inventory及显式export测试真实RUN/PASS、sizes、bench、外部RSS、所有binary和artifacts摘要。执行记录在清单和计时阶段分别保存实际argv、退出码及前后binary/语料摘要，证明两阶段使用同一个已编译对象；不能仅从事后文件推定已执行。普通whole检查的export辅助SKIP不代替显式测量PASS。Go为M1唯一绑定目标；TypeScript/端侧/混合版本及容量门槛另属后续验证。测试完整门禁保留普通20m、expanded fault120m及全部矩阵；最终结果记录在实施报告。
