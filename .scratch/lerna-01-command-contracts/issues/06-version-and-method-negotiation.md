# 06: 版本协商与方法支持清单

**What to build:** 开发者可以协商准确合同版本、profile 和方法 Schema 摘要，再调用已完成的 command.get 合同路径；不兼容版本与未开放方法明确拒绝，类型和夹具升级不会静默扩大服务承诺。

**Blocked by:** 05 — 受信身份与固定 owner 隔离。

**Status:** resolved

- [x] 版本与方法清单明确准确 contract_version、profile、方法及其输入输出 Schema 摘要；Schema 摘要与原命令请求摘要分别定义，不混用两者。
- [x] 在公开协商／验证入口演示“协商同版 → 构造 command.get → 校验受信身份与原 owner → 读取并解码回执和进展”的正常路径；使用可控事实源，不增加真实网络传输。
- [x] 未知或不兼容版本、profile、方法及 Schema 摘要不匹配得到明确拒绝；新增可选字段同样必须通过版本协商，不静默采用默认值或扩大解释范围。
- [x] 未开放方法返回 unsupported，不触发业务处理；task、memory、delegation、Schedule、Environment 等尚未实现的方法不能因设计文档存在就列为支持。
- [x] 支持清单、闭合 Schema、生成 Go／TypeScript 类型及正反例同版；每个被列为支持的方法均有完整输入输出、状态前提、错误和可运行合同证据，半实现 profile 不得广告为可用。
- [x] 明确当前交付是共同信封、command.get 的机器契约与合同验证设施；设计中的 v2-design-1 等占位值不是正式发布版本，合同验证通过不等于生产持久查询已实现。
- [x] CI 运行同版 Go／TypeScript 的正常与拒绝夹具、生成一致性及构建检查；对外说明列出实际支持范围和准确版本，缺少必需套件不得跳过后报成功。
- [x] 本任务记录自身版本协商证据；整个切片仅在全部六个任务验收完成且证据齐全后才能完成，尤其不得用本任务完成替代任务 03 的摘要证据。

## Scope

本任务不实现完整 Application SDK、持久 outbox、真实协议发现或一次性定义未来全部方法。任务 03 可与查询路径独立推进，因此不人为加入本任务的阻塞关系。


## Comments

2026-10-03：依赖01–05已合入，基于集成提交b414092实施。公开交付Go `SupportedMethods` / `Negotiate` 和TS `supportedMethods` / `negotiate`，只广告准确 `1.0.0 / command / command.get`。`methods.json`只登记完整输入／输出根，身份由输入const派生；类型、请求registry、输入输出完整可达Schema摘要由同一生成命令生成，未登记请求不会自动开放。Go返回独立支持副本，TS冻结清单及条目，协商结果不成为权限或业务执行凭据。

公开seam的初始测试在两个入口缺失时red，随后实现最小路径green。15条共同协商正常／拒绝夹具涵盖未知准确版本与设计占位、未开放profile／方法、双方向摘要失配、非法摘要、重复键、未知／缺失字段。Go和TS的正常路径均串起“协商 → 构造command.get → 受信身份／原owner读取 → 解码accepted固定修订5与当前succeeded修订6”；设计方法通过实际GetCommand/getCommand入口得到unsupported，不能进入读取或业务路径。

Schema摘要使用独立域 `lerna-schema-digest-1\n`，从根及全部可达本地定义构建规范bundle。共同 `schema-digests.json` 两项固定golden由独立Python标准JSON／hashlib计算，Go与TS各自从嵌入机器Schema独立遍历、规范化和重算后匹配，不以共同生成常量互相比对替代证据。线上number仍拒绝，机器Schema中的安全非负整数单独处理。公开generator验证13个拒绝探针及8个golden／变更场景：根约束、递归公共定义、深层输出oneOf与新增可选字段会改变相应摘要，键顺序／空白与不可达定义不改变；未登记的未来请求定义不开放。

真实临时工作区的正向生成探针发现pnpm12 exec会尝试隐式安装依赖并拒绝node_modules符号链接。生成器改用锁定安装的prettier CLI绝对路径，临时目录无需安装或共享可写依赖；同一公开生成命令在正常与变更材料均通过。

验证：`make check`通过（只读format、Go vet／test、TS strict与32个测试、生成零差异、158个共同值／命令／响应夹具的Go→TS及TS→Go真实往返、独立46个命令digest案例、28个受信query场景、协商和Schema goldens、双语言构建）。`go test -race ./conformance/component`通过；最后再次运行 `go test ./conformance/component -run Negotiation`、`pnpm check:generated`和`git diff --check`通过。合同1.0.0与生成器1.0.0；环境为锁定Go1.27.1、Node24.19.0、pnpm12.8.1／TypeScript7.0.2。准确代码提交由本票据resolved提交及后续集成merge定位。

证据仅覆盖共同信封、command.get机器合同、受信注入事实源与本地合同验证；本版未发布，生产认证、持久命令账本、网络发现／传输和完整Application SDK均不在交付范围。新增字段即使可选，也不能扩宽已发布旧版含义。票据06完成不替代票据03的命令摘要证据；整片01仍需父任务核对全部六票据、双轴审查与架构优化后记录退出证据。

切片双轴审查回归：生成 Schema 在注册前递归冻结，首次惰性编译前无法改写嵌套额外字段规则，正常请求仍接受、额外字段拒绝，公开 Schema 与固定摘要不变。独立进程公开回归、既有两个 Schema digest goldens、完整 make check / make test-race 均通过，详见 ../code-review.md；不改变准确版本或广告方法。
