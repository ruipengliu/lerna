# 01: 骨架与工具链

**What to build:** 建立 M1 代码骨架的最小可运行形态：单一 Go 模块、Make 命令、静态检查与依赖方向规则、buf 配置、测试规则标注检查。完成后，开发者在空骨架上运行 `make check` 即可得到全部检查结果，后续工单都在这个基础上测试先行。只建本工单需要的目录，不建空目录或占位文件。

**Blocked by:** 无（可以立即开始）

**Status:** resolved

- [x] `make fmt`、`make lint`、`make test`、`make test-fault`、`make gen`、`make check` 可运行，含义与开发规范第 4 节一致
- [x] depguard 规则落实开发规范第 3 节的依赖表；用一个故意违规的导入（核心导入数据库驱动）验证 `make lint` 会失败，验证后删除该导入
- [x] buf 能对一个最小 `.proto` 文件做格式检查、破坏性变更检查和代码生成；`make check` 能发现生成代码过期
- [x] 规则标注检查：开发规范第 5 节列出的包中，没有带 `// 规则：` 标注的测试时 `make check` 失败（包尚不存在时跳过）
- [x] Go 版本用 `toolchain` 指令固定

## Answer

已交付单一 Go 模块、固定工具版本、Make 命令、depguard 与 buf 配置，以及按 Go AST 检查测试函数规则标注的工具。最小协议采用核心契约 2.1 的 `GlobalName`，后续契约工单可直接扩展。

运行证据（macOS arm64、Go 1.27.1）：

- `make check`：通过，包含普通与 fault 构建的 lint、race 测试、规则标注及生成结果比对。尚无 fault 包时明确跳过。
- 红→绿：最初 `make check` 无目标；规则检查测试先因无实现失败，再通过；辅助函数冒充测试的回归测试先失败，再通过。
- 临时在 `core/toolingprobe` 导入 `github.com/mattn/go-sqlite3`，`make lint` 报 depguard 错误；已删除探针并还原临时依赖。
- 将 `GlobalName.user_id` 从 string 改为 int64，buf 对已构建基线报告破坏性变更；已还原。
- 改动生成文件，`make check-gen` 失败；重新生成后通过。检查在临时目录生成，不覆盖开发者工作区。
- 建立无测试的 `core/durable` 临时包，完整 `make check` 在规则标注检查失败；已删除探针。

工具二进制按版本缓存于 `$(go env GOPATH)/bin/lerna-tools`，工作树间复用；首次运行自动安装。唯一运行时第三方依赖为官方 Protobuf 运行时（生成协议必需，无手写序列化替代）。`BUF_BASE` 默认为 main，可设置发布标签；当前 main 尚无协议，初次发布前明确跳过基线比较，之后自动检查。Go 私有 `internal/` 边界由编译器执行，depguard 检查层间依赖。

补充依赖验证：`core/tasks` 导入 `core/sessions`、`defaults/reasoner` 导入 `adapters/api` 的临时探针均被 depguard 拒绝；已清理。核心和可替换模块按设计列出的模块逐一约束；新增模块时需同步增加规则。`make lint BUF_BASE=HEAD` 已验证含协议的 Git 基线读取在独立工作树正常。
