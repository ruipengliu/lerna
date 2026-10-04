# Independent Spec review — slice04 ticket01, intermediate

Fixed range: `64c6872c8ed56ddd66dd51c98dc430046fc9f47d...92d27992f37f0a425621bc75c6d5b4a073fa06a6`; verified merge-base, all four commits and 74 changed paths. Read all fixed changed objects/hunks, eight ACs, originating spec and adopted decisions/handoffs. Generated embedded Go schema was compared fully to the reviewed source; large golden bodies were checked as exact repetitions, with all 101 fixture cases inspected. No tests/build/DB/cleanup or repository changes; no Standards-axis input.

**(a) Missing/partial: 1.**

- **[P2] Direct-source eligibility is absent from Get.** Adopted `.scratch/lerna-04-content-snapshots/contract-shape-decision.md:38` requires “也核全部适用来源当前资格.” `domain/content/service.go:298–314,327–344` checks only the requested target policy and target retention, never `record.Sources`. Publish a directly sourced version, then revoke its source's read permission or shorten source retention: the target remains readable under its unchanged policy. Check the already-supported direct sources before returning bytes; this requires neither inherited closure nor ticket02 propagation.

**(b) Scope creep: 0.** No premature profile advertisement, new Decision methods, or frozen 1.0/1.1 product/schema/fixture/SQL changes found.

**(c) Apparently implemented but wrong: 3.**

- **[P2] New-command aliases bypass original current eligibility.** Adopted decision `:23–24`: “在当前准入资格有效时” and “已经到期…固定expired/forbidden.” `service.go:184–191` immediately accepts an existing matching declaration, bypassing its durable `CurrentRetainUntil` and direct-source checks at `194–224`. A narrowed, expired version after policy widening—or a revoked direct source—still gets a new accepted receipt. Gate association while preserving original receipt/Job.

- **[P2] Replay can disclose receipts after reader expiry.** Adopted decision `:22`: “先当前主体/原命令访问权限.” `service.go:109,116–128` authorizes before waiting for the Command advisory lock; `adapters/postgres/content/policy.go:157–158` samples reader expiry only then. Holding that lock beyond `valid_until` makes the replay return the original receipt without current authority. Recheck after the wait without applying fresh admission expiry to original replay.

- **[P2] Read gates expire during blocking operations.** Adopted decision `:62` requires published “当前获准”; `:85` requires current auth and an independent read cutoff. `service.go:357–380` returns native-read bytes without a subsequent policy gate; `293,405` check AcceptBefore before later locks, never after them. Revocation during object Read still discloses bytes; lock waits can return observations after the read cutoff. Revalidate current qualification and this read's cutoff at the return gate. The evidence already records this fix as pending.

Counts: **a=1, b=0, c=3; worst=P2**. Intermediate findings, not eight-AC acceptance or final delivery; tickets02–06 remain outside this review.
