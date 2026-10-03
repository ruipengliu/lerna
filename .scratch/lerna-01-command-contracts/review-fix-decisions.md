# 切片01审查发现：最小修复决策

2026-10-03。检查固定HEAD `b825c2d59b7b58fd360dd7200d8783a159732de2` 的Go/TS codec、strict JSON parser、canonical writer及生成器，并读取 `/tmp/lerna-standards-probe.go`。本代理只写决策；主代理安排单一fix实施者统一处理审查发现。

## 1. Go Encode必须按紧凑最终正文判断1MiB上限

已复现：合法200223字节通用信封包含200000个`<`，Decode接受，默认json.Marshal将`<`展开为六字节Unicode转义，Encode因此超过1MiB而失败；TS可正常往返。合法U+2028/U+2029也有同类膨胀。

决定复用已有无JSON数字canonical writer生成最终Go线上字节。SetEscapeHTML(false)只解决HTML字符，不能完整解决U+2028/U+2029；全局文本替换转义还可能误改用户正文中的字面量`\\u2028`，禁止采用。

最小实施路径：

1. 沿用typed值的严格预检，尤其CommandPayload原始字节中的重复键、裸数字、无效Unicode和深度检查，不能先序列化修补。
2. 使用现有json.Marshal将生成类型转换成临时JSON表示；这个中间表示不是外部正文，其转义大小不能被误当作最终wire大小。
3. 抽出当前strict parser的私有内部函数，允许Encode为**本地生成的临时表示**使用最多 `6 * MaxBodyBytes` 的有限预算；仍执行同一重复键、数字、Unicode、完整JSON、嵌套规则。公开ParseJSON/Decode签名及1MiB预算绝不放宽。六倍上限来自一个单字节JSON字符可能变成六字节Unicode转义；足以容纳任何原本合法最终正文的默认转义表示，也防止无限临时解析。
4. 对解析后的pure JSON按现有Schema及跨字段规则验证，使用已有canonical writer输出紧凑UTF-8。
5. 对最终输出严格执行 `MaxBodyBytes`，保留现有公开错误映射。最终正文超限仍拒绝。

也可采用等价的更直接typed-to-pureJSON转换，但不建议为了本次修复另写一套反射序列化器或改生成union实现。私有中间预算不是新增公开“宽松解析”入口，不能用于接受外部raw请求。

canonical键排序会改变Go输出的对象键顺序，但不改JSON业务含义；合同没有承诺编码时保留原键顺序。命令摘要既有golden必须完全不变，不能因为复用writer而改变摘要前缀或输入字段。

注意临时转换不可把非法值“转成可解析形式”后放行；现有无效UTF-8、surrogate、重复键、数字、环和过深typed值拒绝测试继续保留。

## 2. TypeScript生成Schema必须递归不可变

已复现：公开导出的schema与Ajv延迟编译使用同一可变对象；首次decodeCommand前将CommandGetPayload.additionalProperties改为true，会接受额外字段，而公开协商摘要不变。这破坏闭合合同和广告准确性。

决定从生成器修复：生成的 `schema` 在导出前递归Object.freeze所有对象及数组，然后交给codec/Ajv。保留原公开schema值与as const类型，增加一个生成文件私有的deepFreeze小函数即可，不增加稳定公开API。不得手改生成物。

选择递归freeze而非仅codec私有clone：clone虽能隔离验证器，仍让公开广告schema可被改成与固定摘要不符。冻结唯一生成源同时保护广告和验证。单层Object.freeze或TypeScript的as const都不足以保护嵌套properties/required/enum。

既有supportedMethods/inputSchemas已冻结其数组及扁平条目，保持现状；不引入通用不可变状态库，不改业务Schema字段或重新定义1.0.0。确保Ajv能够正常编译冻结Schema；当前使用方式只读取源schema，应由已有正常suite验证。

## 3. 可顺手完成的局部去重

TS codec.isWireValue中的surrogate检查可以复用当前json.ts的assertUnicode，保持validate返回boolean语义（捕获此检查失败返回false），原始parseJSON仍按原方式抛错。codec已经依赖json.ts，无需新公共接口或额外模块。此项仅是同一区域的小型去重，可由fix实施者按改动清晰度取舍；不得把它扩展成另一个Unicode/JSON框架。

## 4. 必要回归与验收边界

- 通过公开Go/TS编解码入口，分别覆盖大量`<`、`>`、`&`、U+2028、U+2029，以及临近1MiB的合法紧凑正文。Decode -> Encode -> 另一语言Decode保持准确文本、字符串中真实Unicode与字面量反斜杠转义差异。真正最终正文超过上限依然拒绝。
- 为Go临时表示可能超过1MiB的合法案例提供明确正常对照；不能用提高公开限制让测试变绿。
- TS回归在首次编译CommandGetRequest之前尝试改写公开schema的嵌套additionalProperties；允许freeze抛错或Reflect.set返回false，然后经公开decodeCommand证明额外字段仍拒绝、正常输入仍接受、SupportedMethods摘要与源Schema一致。测试需隔离模块初始化，避免前一测试已编译验证器而掩盖bug。
- 不只断言Object.isFrozen或私有函数调用次数；断言实际公开接纳行为和Schema摘要一致性。没有必要为每个freeze层重复写镜像测试。
- 执行生成一致性、既有Go/TS共同fixture、命令/Schema摘要golden、受信查询正常/拒绝、lint/typecheck/build和make check。缺少现有关键suite不能跳过。

这两处是编码和实现可变性修复，不扩展准确1.0.0机器合同的语义或方法清单；无需新ADR或用户确认。修复后仍需对审查报告其他发现逐项核对，不能用这两项通过代替整体复审。
