## 项目

Lerna 是以可靠性契约为核心的个人 Agent Harness。目前只有设计文档，代码从 M1 开始。

- 技术栈：核心、受信实现和命令行用 Go（单一模块 `github.com/ruipengliu/lerna`）；公共契约用 Protobuf（buf）；SDK 和浏览器界面用 TypeScript，M3 起。见 `docs/adr/0004-language-and-stack.md`。
- 设计是代码的上游：写某个模块前，先读 `docs/architecture/` 中该模块的文档及其上游（项目目标、分层与模块、核心契约）。规则需要改变时，先改设计文档，再改代码。

## 目录骨架

写到哪个模块再建哪个目录，不建空目录或占位文件。

```
contracts/proto/      .proto 源文件（唯一来源）；contracts/gen/go/ 为生成代码，不手改
core/<模块>/           核心模块；私有代码放 core/<模块>/internal/
defaults/             默认推理、默认记忆策略
adapters/             api/ file/ gui/ agent/ interaction/
infra/                受信实现：postgres/ sqlite/ clock/ keys/ egressio/ hosting/ rules/；迁移放各自 migrations/
platform/             gateway/ extensions/ eval/
cmd/                  装配入口：lernad/（云端服务）、lerna/（命令行）；只装配，不写业务规则
conformance/          一致性测试；故障注入放 conformance/fault/（构建标签 fault）
sdk/typescript/       TypeScript SDK（M3 起）
```

`core/<模块>/` 与 `docs/architecture/core/<模块>/README.md` 一一对应。

## 开发规则

- **依赖方向（R6）**：`core/` 只依赖 `contracts/`、标准库、`core/durable` 的事务上下文和本模块声明的端口（见开发规范 3.1）；数据库驱动、网络框架、模型供应商 SDK 只出现在 `infra/`、`adapters/`、`cmd/`。新增第三方依赖要在 PR 中说明理由。
- **测试先行**：先写会失败的测试，再实现。测试函数上方标注它验证的规则，格式固定：`// 规则：G1、开始-2`（编号来自项目目标的 G/R/V 编号和核心契约 2.6 的门禁条件）。涉及外部效果的测试检查持久记录和模拟目标实际收到的调用次数。
- **命名与注释**：标识符用英文，公共对象的字段名、状态名、错误码沿用核心契约；注释用中文，术语以 `docs/architecture/project-goals.md` 第 3 节为准。
- **提交**：Conventional Commits 前缀（`feat:` `fix:` `docs:` `test:` `refactor:` `build:` `chore:`），正文英文；按功能开分支，经 PR 合并到 `main`。只在被要求时提交或推送。
- 提交前运行 `make check`。

工具链、测试分层、协议生成和全部规则见 `docs/development.md`。编写或修改设计文档时，遵守 `docs/architecture/conventions.md`。

## Agent skills

### Issue tracker

Issues and specs live as Markdown files under `.scratch/<feature>/`. See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical triage roles use their default strings: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, and `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

This is a single-context repo using root `GLOSSARY.md` and `docs/adr/`. See `docs/agents/domain.md`. The authoritative glossary is section 3 of `docs/architecture/project-goals.md`; `GLOSSARY.md` only points there.
