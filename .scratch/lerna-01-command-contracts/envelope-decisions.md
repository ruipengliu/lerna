# Ticket02：通用信封的payload边界决策

2026-10-03。依据切片01六张已确认ticket、已有值Schema和生成器。检查工作树 `/tmp/lerna-worktrees/contract-02`，基线45b90ad；不修改产品代码。本决定是实现细化，不需新ADR。

## 采用方案

采用机器Schema定义的通用 `CommandEnvelope`，其 `payload` 引用一个明确命名的动态对象定义；方法验证再使用该方法的闭合Schema。不要另外手写信封字段列表及required判断。

```
"CommandPayload": {
  "type": "object",
  "additionalProperties": true,
  "maxProperties": 1024
}
```

这是未解释业务负载的结构载体，只用在通用信封。生成Go `json.RawMessage`，TS `Record<string, unknown>`。生成器通过准确的资源与 `$defs/CommandPayload` 路径认出这一处特例；不得将“所有additionalProperties=true”都当合法。该节点的shape必须精确符合预期，未知语义关键词照常失败。增加标准 `maxProperties` 的识别和验证；无需引入自定义扩展关键词。

`CommandEnvelope` 本身仍为 `additionalProperties:false`；OwnerRef/ObjectRef/TraceContext仍闭合。已登记方法的输入和payload必须全部闭合，不能引用CommandPayload逃过字段验证。

不必单独建立另一份sourceSchema或手写类型文件：在现有Schema资源或明确新增的commands资源中定义即可；生成器仍从机器Schema产生Go/TS结构。若拆文件，锁定输入列表并明确注册所有本地资源，不能自动扫描网络$ref。

## 为什么使用unknown而不扩建JsonValue

`unknown`表示这个阶段尚未解释值，比any安全；调用方必须完成方法验证才能把payload当作业务类型。为此专门支持递归JsonValue的oneOf/$ref类型图，会增加当前生成器复杂度，不能进一步替代方法验证。

本版所有线上数字仍须是字符串。Record<string,unknown>没有承诺其任意值都可上线，和string类型本身不能表达ID格式一样，公共编码/解析入口负责检查。JSON number、undefined、函数、NaN、Infinity、非法Unicode都不应成为合法原始载荷。

## 必须保持的入口分层

1. `DecodeEnvelope(raw)`：原始字节有界且严格解析（重复键/UTF-8/转义/数字/嵌套等），验证通用Schema，再产生通用信封。只证明格式，不证明任何方法已实现、授权或可执行。
2. `DecodeCommand` / `ValidateCommand(raw)`：先走同一严格入口，然后按(version,profile,method)登记表挑选专用闭合Schema并验证语义。所有Application/Component业务调用必须走此入口，不能把DecodeEnvelope作为执行准入。
3. `command.get`：在方法Schema中固定version/profile/method，使用闭合CommandGetPayload，并禁止expected_revision；target/ref一致性再进行必要跨字段检查。
4. Ticket03摘要可以接受经过 `DecodeEnvelope` 验证的通用命令与受信主体上下文，覆盖expected_revision与任意合法测试业务参数；摘要成功不等于该方法获支持。fixture method使用明显未开放名字亦可，不能把fixture列入支持清单。开放方法实际执行前依然要求完整方法Schema校验。

返回类型/函数名/README必须区分“格式合法的信封”和“受支持的请求”。不要生成名为 `ValidatedCommand` 的通用任意payload类型，使后续runtime误用。Go/TS业务入口不能仅接收任意RawMessage然后略过registry。

## RawMessage和直接对象输入的防绕过

- Go RawMessage只保留字节，不自动带有验证保证。公开EncodeEnvelope及摘要入口必须重走严格解析，拒绝程序直接构造的RawMessage中的重复键、尾随JSON和原始数字；不能只调用json.Marshal后假定安全。
- TS对象入口必须拒绝非JSON值，不能让JSON.stringify把undefined/functions静默删去或NaN转成null后继续验证；优先让公开边界输入raw UTF-8字节/文本。若已有Encode接受对象，沿用并扩展其严格检查。
- 已有公开 `Validate(name, value)` 若允许校验CommandPayload/CommandEnvelope，不得把Schema单独接受数字误表述为完整线上验证。最小方案是为通用入口统一执行已定义的JSON值形状检查（允许null/string/boolean/数组/普通对象，拒绝number及其他值），再执行Schema；这检查的是全协议词法不变量，不再维护一套业务字段规则。或者把Schema-only验证明确作为内部辅助，只通过严格raw入口对外承诺完整验证。
- 编码时不能改变原始文本的业务值；RawMessage里的编码空白可被重新编码，但字符串、大小写、数组顺序及准确数字字符串必须保持。

## 大小限制和生成器回归

原有1MiB原始UTF-8正文、深度64持续约束整个信封及所有payload嵌套。maxProperties=1024约束payload顶层属性数，足以阻止顶层横向膨胀；不假称其递归限制所有对象。嵌套规模由总字节/深度限制，具体方法Schema继续规定字段、数组和长度。

至少新增这些有效/无效对照：空通用payload可解析但未登记method得到unsupported；1024键合法/1025拒绝；command.get未知payload键拒绝；通用payload中的嵌套重复键、9007199254740993裸数字、程序构造RawMessage绕过和NaN/undefined绕过拒绝；通用expected_revision可参与摘要而command.get出现它必须拒绝。

生成器检查要证明：允许命名CommandPayload特例；另一任意object设additionalProperties:true仍硬失败；方法Schema误引用CommandPayload不应被登记为闭合可支持的方法。
