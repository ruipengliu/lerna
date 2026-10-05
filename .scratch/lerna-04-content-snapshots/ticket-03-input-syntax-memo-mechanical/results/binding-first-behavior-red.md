# Binding Revision 指针：真实首 RED

格式化实际0；仅测试proposal从3060B/86b3e1d4变为3128B/788392c8，完整format diff保存。产品源码和当前331的全部pre/post hashes不变，undefined Input测试未进入编译。

normal实际FAIL，包用时0.012s：operation_revision与task_revision各 cold/hot 返回污染后，四个实际断言都观察到下一次原字节读取的Binding被改变；decoder error均nil，正常 cold/hot counterpart原完整表示比较先通过。nil_counterpart实际PASS。测试体真实进入，不是编译/setup错误；该 RED 是既有 Binding深拷贝隔离缺口，不是Input memo性能或业务容量验收。

失败子例在最终parse-count断言之前退出，因此这次不单独声称 raw-byte key 所有权控制 green；原body变更实际已经发生，完整事实保留raw。

| 实际阶段 | PID/PGID | start tick | exit | Wait/current group |
| --- | --- | --- | --- | --- |
| formatter | 3992592 | 16744553 | 0 | actual ACK / empty |
| normal | 3992623 | 16744570 | 1 | actual ACK / empty |

完整原日志：`/workspace/lerna-content-03-137311247276/context-binding-revision-pointer-first-red-normal.log`，20行/1626B，SHA256 b44a4dfb463feb2b32cb39b94ce72d3be076ecb7c733f6c0b3aa1beba6f21f1d。formatter原raw1行/39B/06fd0fb3。两raw own writer各fsync/firstClose ACK；两个原PGID现有member[]。这不倒补旧Go输出FD、testing CPU descriptor、fixture holder的UNKNOWN。

实际outcome保留embeddedprelaunch原事实；reviewed文件另加完整raw阅读与界限，未洗旧登记。已STOP / RELEASE LOCAL，未修源码、未重试、未执行Input compile RED/任何新Go或PG。
