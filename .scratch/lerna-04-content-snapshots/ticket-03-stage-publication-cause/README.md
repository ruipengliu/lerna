# StagePublication：真实取消原因首 red

固定原产品28e；两份新增tracer/support是本次真实编译的新源码，不谎称在28e提交内。原始静态清单、格式后精确源码和原日志分开保存。fmt actual3396642/start14218216/0/absence；业务actual3397277/start14220903/session72752/1/absence，Go1.940s，原caller30、事务3/statement2/lock1、test/outer120不变。

root全文读取原日志及两份源码。正常真实Adapter.Publish/ReadPublished和reopen通过1.16s；故障经过原input真实PG row wait后取消实际publish context，join两actor，read-only locker按原Store.within退出，两个peer Close成功。当前受权公开查询和重开后均确认原发布Command未被接纳，最后断言才实际失败：返回decision authorization denied（ErrForbidden），context.Canceled原因丢失。不是编译、准备或外层timeout失败。

采用已读独立Standards P2的最小修正方向：确切sql.ErrNoRows映射ErrForbidden，其余真实错误保留cause。仅静态授权，尚无修正后的green/race；性能优化独立分列。原所有未知资源保持，Wait/组absence不替代逻辑Close；本目录不认定七AC或整票完成。

## 同原反例修正后的资格

root全文fixed normal/race各原日志与provenance，两模式实际0/absence：normal1.801s(native3406457/start14261484/session87137)，race5.731s(3407442/start14265022/session89145)，无DATARACE/timeout。真实PG对557855/557856与558038/558039锁等待后取消均保留context.Canceled原因，两peer Close/双join/publicNotFound/reopen尾执行通过。准确HEAD28e加两product WIP（closure局部纯身份复用及3行cause处理）与两new tracer source；完整格式后hash记录并独立核对，不能把WIP归成原28e提交。这里只资格cause出口，新增closure容量未跑，收益不归cause。
