# 01: 骨架与工具链

**What to build:** 建立 M1 代码骨架的最小可运行形态：单一 Go 模块、Make 命令、静态检查与依赖方向规则、buf 配置、测试规则标注检查。完成后，开发者在空骨架上运行 `make check` 即可得到全部检查结果，后续工单都在这个基础上测试先行。只建本工单需要的目录，不建空目录或占位文件。

**Blocked by:** 无（可以立即开始）

**Status:** ready-for-agent

- [ ] `make fmt`、`make lint`、`make test`、`make test-fault`、`make gen`、`make check` 可运行，含义与开发规范第 4 节一致
- [ ] depguard 规则落实开发规范第 3 节的依赖表；用一个故意违规的导入（核心导入数据库驱动）验证 `make lint` 会失败，验证后删除该导入
- [ ] buf 能对一个最小 `.proto` 文件做格式检查、破坏性变更检查和代码生成；`make check` 能发现生成代码过期
- [ ] 规则标注检查：开发规范第 5 节列出的包中，没有带 `// 规则：` 标注的测试时 `make check` 失败（包尚不存在时跳过）
- [ ] Go 版本用 `toolchain` 指令固定
