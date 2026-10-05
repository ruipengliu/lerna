# Spec — ticket06 final frozen producer candidate

固定 `/tmp/lerna-worktrees/content-snapshots-06` baseline `dadd801cfd11ca038f0496872941097f10ebe507`、HEAD `c11fd2092875417d039914f8ecbd083a7a846d3a`，叠加 manifest 冻结 WIP `c9a09814…`。审查命令：`git diff dadd801cfd11ca038f0496872941097f10ebe507...HEAD -- conformance/component/content_process_recovery_test.go conformance/internal/contentfixture/process_borrow.go conformance/internal/contentfixture/world.go`；再执行同三路径的 `git diff HEAD -- …`。七提交 `c11fd20/d9dce0c/60f6c65/3d60d1e/1dd6311/99cd825/4620ac3`；准确命令、pins 与合成 diff 见 `/tmp/lerna-06-final-fixed-review/manifest.json`。

独立 Spec STATIC 轴。依据 `.scratch/lerna-04-content-snapshots/spec.md`、`issues/06-publication-process-recovery.md`（issue）和采用 `/tmp/lerna-06-final-qualification-scope-decision.md`。先后独立核三源和三个 diff 的 bytes/SHA；完整读1746行父/共享producer与两个 fixture 源，结束三 pins/HEAD未变。未读 Standards，未运行 native/Go/Node/DB/fmt、修改产品或整合。

**代码 findings：0。** (a) 未发现缺失的实现；(b) 未发现未经要求的范围扩张；(c) 未发现错误实现。

issue9–10要求“安装耐久确认后、元数据发布前…SIGKILL/Wait/EOF”及“提交前rollback、发布后答复丢失…正常对照”。gate分别在真实 local.Put成功返回、真实发布callback完成但未Commit、真实Core.Within成功Commit后；父验收公开 preparing/published、固定receipt/ref和独立完整字节。SIGKILL后原Job/staging恢复、自然原lease到期及重开均保留，没有靠重造请求或私表状态冒成功。

issue11要求“无法合法发布走05原对象清理/残留出口”。policy-orphan以真实当前Save撤回使原恢复失败，随后准确SealOrphan→原staging/primary责任→真实独立物理核验/allACK→重开Gone；固定failed进度与另一原policy责任保持。按采用范围，05已资格的secondary/offline/ENOTEMPTY产品未变，不添加child×secondary矩阵，也不声称该组合实际执行。

issue12要求“真正旧worker迟到安装/完成不越过…对象fence”。两个live旧worker场景在原I/O4保守窗口内，对照真实正常完成和原allACK后的late-Finish/actual local.Put body_sealed；不Kill、续Claim、热换Publisher或放宽截止。issue13–14要求“所有FD/native holder实际退出后才能清理”与“SIGKILL不冒称掉电”。每generation在Start前借用、pre-open登记身份；首Close帧、Wait、pipe Stop各独立。killed原首Close永久UNKNOWN，World保留对应scope；配置缺失硬失败，物理ACK不被进程absence替代。

**已知未完成资格：1组，非代码缺陷。** spec62要求“全部验收通过并附准确版本、环境、命令、结果和限制后”才完成。固定 manifest 仅记当前producer首normal PASS；其余14资格、最终检查/资源审计/最终integration pin与root接受仍待。本审查不替代这些执行，不宣告六AC resolved、whole04退出或完整1.2开放；准确历史仍22/41、广告OFF。
