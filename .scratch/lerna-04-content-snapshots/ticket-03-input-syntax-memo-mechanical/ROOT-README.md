# Input 语法复用：机械资格

root 核对 [61份归档原件](archive-proposal-index.json)、10次原执行、285行完整日志及 [准确登记区间](native/ledger-interval-provenance.json)。原Binding指针污染的四个cold／hot断言真实失败；最小深拷贝修正后原五测试与新指针测试正常／竞态通过。Input 首次失败仅为9条缺 `inputDecodeMemo` 的编译诊断，测试体未执行，不能称解码行为 RED。

后续最小成功语法缓存接入原完整 SQL／FOR UPDATE 成功读取之后。仅Store实例、完整owner／schema、Input ID及全部原字节相同时复用一项成功表示；返回独立深拷贝，不缓存权限、预算、时钟或事务结果。原错误和部分输出保留。两个产品文件格式化无差异，当前完整332项清单核对一致。

本轮正常／竞态各12个顶层测试及36个子测试全部通过：作用域／完整字节／空白变化、原严格错误及1MiB边界、单条替换、nil／empty、两Revision指针与全部可变返回值、有限并发。root 全文读取本轮199行及此前86行原日志，核对各原Wait、登记身份、当前组为空和自身日志writer首次Close；它们不代替历史对象holder的Close。

真实PG热输入修订、原attempt预算拒绝、政策查询错误与当前权威资格仍待；容量原失败、首次Put效果UNKNOWN及旧FD Close UNKNOWN保留。此处不证明净收益、原Claim5秒容量通过、票03或切片04退出。两个新增测试当前仍为准确虚拟overlay，产品源仍包含已记录的此前clock／BodySeal工作区改动，不能把仅三文件提交冒称重现整个受测树。完整1.2仍关闭。
