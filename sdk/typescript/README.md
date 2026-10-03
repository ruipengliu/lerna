# TypeScript 公共合同

当前私有包 `@lerna/contract` **1.0.0** 提供[公共合同](../../contract/README.md)、严格原始 JSON 编解码、原命令摘要、固定回执／当前进展解码，以及受信注入事实源的 `getCommand`。唯一支持路径是 `1.0.0 / command / command.get`；`supportedMethods` 和 `negotiate` 要求准确版本及输入／输出 Schema 摘要。材料尚未作为发布包上线，设计占位版本和未完成方法不开放。

`src/generated/` 来自同版 JSON Schema 与明确方法清单；公开入口为 `src/index.ts`。`parseCommand` 只验证通用信封，成功不证明方法已开放；`decodeCommand` 验证具体方法。`commandDigest` 绑定原业务内容与受信主体，不授予权限。`getCommand` 要求宿主注入已认证主体、读取授权、准确原 owner 目录、时钟和有限 `maxReadDurationMs`，不会从 payload 取得可信身份或改变原命令身份。`decodeCommandResponse` / `encodeCommandResponse` 保持固定接纳事实与当前进展的差异。

本包没有生产认证服务、持久命令账本、网络传输、Task 推进或完整 Application SDK。身份与目录验证的证据来自受信可控端口；协商通过不等于生产持久查询已实现。入口前提、拒绝分类、原 owner、期限、摘要算法及证据见[合同说明](../../contract/README.md)。

在仓库根目录执行 `make bootstrap`、`make check`。`make build` 生成 `dist/` 的 JavaScript 和声明文件，输出不提交。TypeScript 开启 strict，外部未知值必须先经过 `decode`，不得用类型断言绕过验证。`ContractError.code` 为闭合错误代码；发送错误时使用 `encode('PublicError', error.toPublicError())`，不输出本地 `cause`。
