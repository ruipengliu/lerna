本轮仅语义资格：fmt、normal、race 均实际 exit 0 / Wait ACK / 原进程组空，已 STOP / RELEASE LOCAL。183 行原 raw 全读；normal 与 race 各 12 top + 18 sub PASS，包时间 20.592s / 36.109s。产品源332逐项 pre=post，原3proposal中只有新政策锁测试格式变化（完整8551B diff），另外两份字节未变。

| 控制 | 实际结果与原尾 |
| --- | --- |
| 同 dispatcher 的 Input revision | 原 first attempt ReserveRead seq7 返回 ErrChanged + empty；拒绝前后完整公开 budget 相同；真实第二轮当前 Input/Mandatory 全量匹配；Reopen 第三轮及第四拒绝，最终3轮18 reads、reserved=confirmed=27926、unknown=0、原上限65536 |
| 原 budget lock deadline | 同 caller warm Input 后真实 PG root lock 等待；原2s deadline 过期后 rounds=0/reserved=0；peer firstClose ACK；正常对照、NoDispatch/Reopen 全尾保持 |
| 原并发预算 | 原最后一轮两 contender 1 accepted/1 refused，总 rounds=3；原6 byte 两 contender 1 accepted/1 refused/reserved=confirmed=6/physical_reads=1 |
| 原当前授权与 clock | decoded Binding 正常/process撤销/worker-control；record-wait metadata顺序、锁后clock和过期、target/ancestor BodySeal 与 post-I/O Process withdrawal 的原正常/拒绝/公开尾全部 PASS |
| 新 ancestor policy query error | 原实际 HoldPolicy blocked 证明；normal-release full ProcessingRead 与 before 相同；fault 原1s lock_timeout 自然返回 *pgconn.PgError/SQLSTATE55P03、caller_live=true、结果全零；actor joined、原 holder/observer first release ACK。两分支原alpha正文、固定Put回执、Get published历史、释放后完整恢复及 Reopen 尾先执行，最后分类断言 PASS |

资格范围没有扩展到 TCP/网络断连或全部 transport 故障；没有新增 TrustedUntil 自然到期用例；revision 拒绝不证明下游 digest/control 分支都已执行。本轮没有容量验证或净收益承诺。原 profile/Go FD Close UNKNOWN、历史 Put cause/effect UNKNOWN 与所有旧 FAIL 原件保持；新 own raw fsync/firstClose ACK 与原进程组空不回填历史或所有 holder ACK。

完整身份/prelaunch/current332/formed3/raw SHA 保存在 semantic-qualification-actual-outcome.json；有限 machine 结果 semantic-results.json，精确格式差量 actual-three-test-format.diff，formed 源绑定 formatted-source-manifest.json。三实际 PID/tick：fmt4126582/17304109，normal4126600/17304127，race4127455/17306887。
