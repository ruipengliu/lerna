# 切片01真实CI故障：Go格式文件发现

2026-10-03。已读取当前scripts/check-go-format.mjs与Makefile。主任务从GitHub connector取得真实run日志：remote70cbf66的job111244107202在make check首步因spawnSync rg ENOENT失败；工具bootstrap成功。先前gh API Forbidden只表示那条读取路径受限，不能继续把全部远端CI状态写成未核实。

## 最小修复决定

替换未声明的rg依赖，使用已是Gitcheckout/仓库操作前提的Git：

```
git ls-files -z --cached --others --exclude-standard -- '*.go'
```

继续使用execFileSync和独立参数数组，不经shell插值。按NUL分割并去除唯一空末项，不能trim列表或按换行分割；必须保留含空格/换行的准确路径。列表同时覆盖已跟踪和未忽略的未跟踪Go文件，尊重仓库ignore规则，不自行实现Git忽略匹配。

在仓库根执行，与现有Makefile入口一致。传给gofmt的路径可统一加 `./`，避免合法的以连字符起始的文件名被当作flag。若跟踪文件在工作树已删除，仅对该ENOENT按不在工作树处理；其他文件读取/执行错误不能吞掉。空列表不调用无文件参数的gofmt，避免意外读取stdin。gofmt仍以只读 `-l` 检查，存在任何未格式文件则明确非零退出；工具缺失或Git命令失败也必须失败。

不为本次修复增加apt安装rg，不引入Node递归遍历或通用文件发现框架。rg仍可作为开发搜索工具，但不成为未声明的CI构建依赖。

## 验证及交付

在临时PATH只提供node/git/gofmt（明确没有rg）的环境执行该脚本，正常已格式文件成功；临时创建一个未跟踪、未格式的.go文件，必须非零拒绝，随后清理；带空格文件名也要以一个参数被检查。使用既有lint/check流程确认正常仓库无回归。测试不读取或输出环境凭据、DSN或GitHub令牌。

由统一fix实施者处理此真实build缺陷，独立于harness架构优化提交。推送修复后，主任务用已可用connector核对**新准确HEAD**的真实GitHub run结果，不能只以本地通过或旧bootstrap成功宣布CI绿色。切片01退出记录更新该真实结果并保留旧失败及修复依据。

这是工具依赖修复，不改变1.0.0合同、不新增ADR。当前未修改产品或脚本代码，只记录裁决。
