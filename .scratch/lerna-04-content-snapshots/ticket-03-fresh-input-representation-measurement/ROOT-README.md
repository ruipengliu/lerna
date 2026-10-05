# Fresh Input 测量：容量仍失败

原一次竞态执行 `3948276`／start tick `16546665` 实际 exit1、Wait 已返回、当前原进程组为空。root 全文读取320行原日志，并核对6个scope的154个非空成本单元、源码／8个overlay／独立二进制前后身份。完整结果见 [测量说明](artifacts/README.md) 与 [root 核对](root-independent-archive-audit.json)。原 caller30秒、Claim5秒、包120秒均不变。

closure64 失败：compile12.622s，Step5.059s，首次47个材料读取耗尽Claim，caller尚余2.365s，无Prepared或发布成功。static62 失败：compile11.346s，Step5.129s，Prepared已提交，首次Put返回原错误，外部效果仍UNKNOWN；Claim结束晚63.567ms，没有Completed、发布ACK或readback。四个原拒绝控制通过。此结果与前次caller耗尽分开记录。

成功解码成本有明确观察，父阶段包含子阶段，不能相加推断总CPU或数据库服务器成本，observer额外开销未知。下一项已采用决定仅为 Input 成功语法的单条、完整原字节匹配复用；每次仍完整SELECT／FOR UPDATE、当前权威与tuple／digest检查，不承诺容量一定通过。

67份原件共3159046字节与归档逐项一致。原索引错误把三条实际 native 记录标成完整全局文件；[原索引](original-ambiguous-proposal-index.json) 保留。新 [索引](archive-proposal-index.json) 引用不可变捕获前缀及准确连续区间；[修正依据](append-mismatch-and-exact-prefix-provenance.json) 区分当时三条执行事实与后来追加的Binding资格，不伪造历史全局哈希，不补证未知首次Close。完整二进制及331份产品源码不重复纳入归档。

此处仅记录失败测量与后续决定，票03及切片04未退出，完整1.2仍关闭。
