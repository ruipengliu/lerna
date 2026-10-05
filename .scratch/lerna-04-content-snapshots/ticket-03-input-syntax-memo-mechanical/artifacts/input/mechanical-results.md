# Input 语法 memo：有限机械资格

唯一两个产品文件fmt实际0且0diff。dispatcher仍 c1042da40d5ab316d4bb788d7b866c4f1085ac595508a7751524d3bcb5c0161b/23512B；新helper仍0fa3cc7acc76bc3d16c5fde7e6b5a45371f1aca2508bc35c781ae8287b3f256f/1323B。完整current332每阶段pre/post完全相等，其他原330源不变，已qualified Binding cloneInput helperfe6与两virtual test源788392/abd495保持。

normal实际PASS0.070s；race实际PASS1.533s，各12top+36sub全部PASS，无DATA RACE/Go超时。实际进入Input六个top测试：exact full-row bytes与caller原body隔离、所有mutable cold/hot返回污染（含两Revision指针）、nil/present-empty、完整scope/仅空白变化与单项替换、strict失败重复不缓存/原typed partial output/1MiB边界、有限8×20并发。原Binding五控制+新pointer正常对照也保持。

这些结果只证明私有语法复用与返回所有权；parse次数不是公开权限或性能oracle，也不保证并发cold miss exact-once。实际dispatcher保留每次原SQL/Scan的接法已静态逐字核对，但本组没有真实PG fresh row/锁/取消/授权/预算reopen资格，更没有原大图容量或净收益通过。

| 阶段 | PID/PGID | start tick | actual exit | Wait/current group |
| --- | --- | --- | --- | --- |
| fmt2 | 4045505 | 16974337 | 0 | ACK / empty |
| normal | 4045512 | 16974354 | 0 | ACK / empty |
| race | 4045654 | 16974703 | 0 | ACK / empty |

全部199行raw已FULL阅读，路径OWNROOT/context-input-memo-{format,normal,race}.log。normal99行/7137B SHA80096f20c820bf997189f4a106f59fb6586c1cd402ac3c9ab0ca0f60fd296eb3；race99行/7137B SHA16e808497923075b275d6a6b3b6addcea021694e8aeca754f5e7a248ce2f8532。每阶段prelaunch原argv/source/身份scope、own raw writer fsync/firstClose、actualnativeWait及当前组empty分别保存。

原真实Binding行为RED和诚实Input compileRED、原大图FAIL、原Go/CPU FD Close UNKNOWN、firstPut原因/possibleeffect UNKNOWN以及observer总开销UNKNOWN均保持，不能用新ACK补证。已STOP / RELEASE LOCAL；没有自动PG/容量/新native或修产品。票03与whole04退出尚未由此证明。
