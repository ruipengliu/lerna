# TypeScript SDK

独立 workspace 包 `@lerna/sdk`，不依赖 React。当前只提供 `lerna-dev-status/1` 本地诊断读取，不能用于提交或恢复 harness/1 业务命令。

workspace 直接消费 TypeScript 源码以支持本地开发，`pnpm --filter @lerna/sdk build` 另输出 JS 和声明到 `dist/`。包当前为 private；正式发布时再切换 exports 到已验证的发行产物，并引入同版契约生成、严格解析和 WSS/IndexedDB 恢复。
