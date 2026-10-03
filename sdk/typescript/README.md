# TypeScript 公共合同

当前包 `@lerna/contract` 提供 [公共值合同 1.0.0](../../contract/README.md) 和严格原始 JSON 编解码。`src/generated/` 完全来自同版 JSON Schema；手写校验入口在 `src/index.ts`，原始词法边界在 `src/json.ts`。无连接、存储、身份认证或 Task 推进实现。

在仓库根目录执行 `make bootstrap`、`make check`。`make build` 生成 `dist/` 的 JavaScript 和声明文件；构建输出不提交。TypeScript 开启 strict，外部未知值必须先经过 `decode`，不得用类型断言绕过验证。

命令通用语法入口为 `parseCommand`，具体已登记方法入口为 `decodeCommand`；前者成功不表示该方法已开放。当前后者仅验证 `1.0.0 / command / command.get` 请求，不读取事实或执行业务。`ContractError.code` 为闭合错误代码；发送错误时使用 `encode('PublicError', error.toPublicError())`，不输出本地 `cause`。完整边界见 [公共合同说明](../../contract/README.md)。
