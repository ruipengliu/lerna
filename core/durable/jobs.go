package durable

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"time"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ids"
)

// JobState 是待办工作的状态（持久工作 2.2）。
type JobState string

const (
	JobReady   JobState = "READY"
	JobClaimed JobState = "CLAIMED"
	JobWaiting JobState = "WAITING"
	JobBlocked JobState = "BLOCKED"
	JobDone    JobState = "DONE"
	JobSealed  JobState = "SEALED"
)

// never 是"等待唤醒"的最早领取时间。
const never = int64(math.MaxInt64)

// JobSpec 描述一项待办工作。
type JobSpec struct {
	Kind string
	User string
	// Subject 是责任对象：原任务、动作、尝试或交接的标识。
	Subject string
	// PurposeKey 是唯一的工作目的键（所属模块 + 责任对象 + 推进目的），防止重复通知产生重复工作。
	PurposeKey string
	// Spec 是固定版本的工作规格。
	Spec      []byte
	NotBefore time.Time
	// Wait 为真时工作创建为等待状态，直到被唤醒。
	Wait bool
}

// EnqueueJob 在事务中登记待办工作。目的键已存在时返回已有工作，不再创建。
func (t *Tx) EnqueueJob(s JobSpec) (string, error) {
	var existing string
	err := t.QueryRow(`SELECT job_id FROM jobs WHERE user_id = ? AND purpose_key = ?`, s.User, s.PurposeKey).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	id := ids.New()
	state := JobReady
	notBefore := t.NowMs()
	if !s.NotBefore.IsZero() {
		notBefore = s.NotBefore.UnixMilli()
	}
	if s.Wait {
		state = JobWaiting
		notBefore = never
	}
	_, err = t.Exec(`INSERT INTO jobs (job_id, user_id, kind, subject, purpose_key, spec, revision, state,
		not_before, claim_epoch, claim_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?, 0, 0, ?, ?)`,
		id, s.User, s.Kind, s.Subject, s.PurposeKey, s.Spec, string(state), notBefore, t.NowMs(), t.NowMs())
	return id, err
}

// WakeJob 唤醒等待中的工作（等待 → 待领取）。工作不存在或不在等待时不做任何事。
func (t *Tx) WakeJob(user, purposeKey string) error {
	_, err := t.Exec(`UPDATE jobs SET state = ?, not_before = ?, updated_at = ?
		WHERE user_id = ? AND purpose_key = ? AND state = ?`,
		string(JobReady), t.NowMs(), t.NowMs(), user, purposeKey, string(JobWaiting))
	return err
}

// SealJob 封闭一项工作：所属模块确认这项工作不必继续。终态不会重新打开。
// 封闭"发送工作"不会封闭已经发出的动作，也不结束对它的核对责任。
func (t *Tx) SealJob(user, purposeKey string) error {
	_, err := t.Exec(`UPDATE jobs SET state = ?, revision = revision + 1, updated_at = ?
		WHERE user_id = ? AND purpose_key = ? AND state NOT IN (?, ?)`,
		string(JobSealed), t.NowMs(), user, purposeKey, string(JobDone), string(JobSealed))
	return err
}

// ReviseJob 修订工作规格：工作修订号递增，旧修订号的领取无法再推进。
func (t *Tx) ReviseJob(user, purposeKey string, spec []byte) error {
	_, err := t.Exec(`UPDATE jobs SET spec = ?, revision = revision + 1, updated_at = ?
		WHERE user_id = ? AND purpose_key = ? AND state NOT IN (?, ?)`,
		spec, t.NowMs(), user, purposeKey, string(JobDone), string(JobSealed))
	return err
}

// UnblockJob 由所属模块显式恢复受阻的工作（受阻 → 待领取），责任不变。
func (t *Tx) UnblockJob(user, purposeKey string) error {
	_, err := t.Exec(`UPDATE jobs SET state = ?, not_before = ?, updated_at = ?
		WHERE user_id = ? AND purpose_key = ? AND state = ?`,
		string(JobReady), t.NowMs(), t.NowMs(), user, purposeKey, string(JobBlocked))
	return err
}

// JobInfo 是查询工作的结果。
type JobInfo struct {
	JobID      string
	Kind       string
	Subject    string
	PurposeKey string
	State      JobState
	Revision   int64
	Epoch      int64
	NotBefore  time.Time
	LastError  string
	ClaimCount int
}

// JobByPurpose 查询工作当前状态。只读，不触发重试或重新派发。
func (t *Tx) JobByPurpose(user, purposeKey string) (*JobInfo, error) {
	return scanJob(t.QueryRow(`SELECT job_id, kind, subject, purpose_key, state, revision, claim_epoch, not_before,
		COALESCE(last_error, ''), claim_count FROM jobs WHERE user_id = ? AND purpose_key = ?`, user, purposeKey))
}

func scanJob(row *sql.Row) (*JobInfo, error) {
	var j JobInfo
	var state string
	var nb int64
	err := row.Scan(&j.JobID, &j.Kind, &j.Subject, &j.PurposeKey, &state, &j.Revision, &j.Epoch, &nb, &j.LastError, &j.ClaimCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	j.State = JobState(state)
	j.NotBefore = time.UnixMilli(nb)
	return &j, nil
}

// Claim 是一次领取：领取代次和工作修订号共同决定能否推进。
type Claim struct {
	JobID      string
	Kind       string
	User       string
	Subject    string
	PurposeKey string
	Spec       []byte
	Revision   int64
	Epoch      int64
	Instance   string
	LeaseUntil time.Time
	ClaimCount int
}

// ClaimJobs 原子领取最多 max 项可领取的工作（持久工作 4.4）。
//
// 每次授予领取都递增领取代次；租约过期的工作被原子回收。领取回执按 claimID 保存：
// 用原标识重试返回原清单；拿回回执不等于恢复领取权。
func (d *Domain) ClaimJobs(ctx context.Context, claimID, instance string, max int) ([]Claim, error) {
	if prior, ok, err := d.claimReceipt(ctx, claimID); err != nil {
		return nil, err
	} else if ok {
		return prior, nil
	}
	d.mu.RLock()
	kinds := make([]string, 0, len(d.jobs))
	for k := range d.jobs {
		kinds = append(kinds, k)
	}
	d.mu.RUnlock()
	var claims []Claim
	err := d.Write(ctx, "durable:claim", func(tx *Tx) error {
		claims = nil
		now := tx.NowMs()
		rows, err := tx.Query(`SELECT job_id, kind, user_id, subject, purpose_key, spec, revision, claim_epoch, claim_count
			FROM jobs
			WHERE (state IN (?, ?) AND not_before <= ?) OR (state = ? AND lease_until <= ?)
			ORDER BY not_before, created_at, job_id`,
			string(JobReady), string(JobWaiting), now, string(JobClaimed), now)
		if err != nil {
			return err
		}
		known := map[string]bool{}
		for _, k := range kinds {
			known[k] = true
		}
		for rows.Next() && len(claims) < max {
			var c Claim
			if err := rows.Scan(&c.JobID, &c.Kind, &c.User, &c.Subject, &c.PurposeKey, &c.Spec, &c.Revision, &c.Epoch, &c.ClaimCount); err != nil {
				_ = rows.Close()
				return err
			}
			if !known[c.Kind] {
				continue
			}
			claims = append(claims, c)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		lease := tx.Now().Add(d.LeaseDuration)
		for i := range claims {
			claims[i].Epoch++
			claims[i].ClaimCount++
			claims[i].Instance = instance
			claims[i].LeaseUntil = lease
			if _, err := tx.Exec(`UPDATE jobs SET state = ?, claimer = ?, claim_epoch = ?, lease_until = ?,
				claim_count = ?, updated_at = ? WHERE job_id = ?`,
				string(JobClaimed), instance, claims[i].Epoch, lease.UnixMilli(), claims[i].ClaimCount, now, claims[i].JobID); err != nil {
				return err
			}
		}
		blob, err := encodeClaims(claims)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO claim_receipts (claim_id, instance, claims, created_at) VALUES (?, ?, ?, ?)`,
			claimID, instance, blob, now)
		return err
	})
	if err != nil {
		return nil, err
	}
	return claims, nil
}

func (d *Domain) claimReceipt(ctx context.Context, claimID string) ([]Claim, bool, error) {
	var blob []byte
	found := false
	err := d.Read(ctx, func(tx *Tx) error {
		err := tx.QueryRow(`SELECT claims FROM claim_receipts WHERE claim_id = ?`, claimID).Scan(&blob)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		found = err == nil
		return err
	})
	if err != nil || !found {
		return nil, false, err
	}
	c, err := decodeClaims(blob)
	return c, true, err
}

// Renew 续租：必须仍是有效领取；不递增领取代次，不改变工作修订号。
func (d *Domain) Renew(ctx context.Context, c *Claim) error {
	return d.Write(ctx, "durable:renew", func(tx *Tx) error {
		if err := checkClaim(tx, c); err != nil {
			return err
		}
		until := tx.Now().Add(d.LeaseDuration)
		if _, err := tx.Exec(`UPDATE jobs SET lease_until = ?, updated_at = ? WHERE job_id = ?`,
			until.UnixMilli(), tx.NowMs(), c.JobID); err != nil {
			return err
		}
		c.LeaseUntil = until
		return nil
	})
}

// Transition 是一次推进之后工作的去向。
type Transition struct {
	state     JobState
	notBefore int64
	reason    string
	keep      bool
}

// Done 表示这项推进已完成。工作完成不代表任务成功。
func Done() Transition { return Transition{state: JobDone} }

// WaitUntil 表示工作等到 at 再领取。
func WaitUntil(at time.Time, reason string) Transition {
	return Transition{state: JobWaiting, notBefore: at.UnixMilli(), reason: reason}
}

// WaitWake 表示工作等待所属模块唤醒。
func WaitWake(reason string) Transition {
	return Transition{state: JobWaiting, notBefore: never, reason: reason}
}

// Block 表示工作受阻：达到恢复上限、版本不支持或缺少必要证明。责任保留。
func Block(reason string) Transition { return Transition{state: JobBlocked, reason: reason} }

// Keep 表示保持领取，处理函数还要继续推进。
func Keep() Transition { return Transition{keep: true} }

func checkClaim(tx *Tx, c *Claim) error {
	var state, claimer string
	var epoch, revision, lease int64
	err := tx.QueryRow(`SELECT state, COALESCE(claimer, ''), claim_epoch, revision, COALESCE(lease_until, 0)
		FROM jobs WHERE job_id = ?`, c.JobID).Scan(&state, &claimer, &epoch, &revision, &lease)
	if errors.Is(err, sql.ErrNoRows) {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_STALE_CLAIM, "job %s not found", c.JobID)
	}
	if err != nil {
		return err
	}
	// 状态为已领取 ∧ 进程实例匹配 ∧ 领取代次匹配 ∧ 工作修订号匹配 ∧ 租约未过期（持久工作 4.4）。
	if state != string(JobClaimed) || claimer != c.Instance || epoch != c.Epoch || revision != c.Revision || lease <= tx.NowMs() {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_STALE_CLAIM,
			"job %s: claim epoch %d revision %d no longer current (state %s epoch %d revision %d)",
			c.JobID, c.Epoch, c.Revision, state, epoch, revision)
	}
	return nil
}

// Advance 在一个事务里核验当前领取，执行所属模块的业务修改，并转换工作状态（持久工作 4.4）。
// 核验和写入在同一事务内完成；旧领取、旧修订号返回 STALE_CLAIM。
func (d *Domain) Advance(ctx context.Context, c *Claim, label string, fn func(tx *Tx) (Transition, error)) error {
	return d.Write(ctx, label, func(tx *Tx) error {
		if err := checkClaim(tx, c); err != nil {
			return err
		}
		tr, err := fn(tx)
		if err != nil {
			return err
		}
		return applyTransition(tx, c, tr)
	})
}

func applyTransition(tx *Tx, c *Claim, tr Transition) error {
	if tr.keep {
		return nil
	}
	nb := tr.notBefore
	if nb == 0 {
		nb = tx.NowMs()
	}
	_, err := tx.Exec(`UPDATE jobs SET state = ?, not_before = ?, claimer = NULL, lease_until = NULL,
		last_error = ?, updated_at = ? WHERE job_id = ?`,
		string(tr.state), nb, nullIfEmpty(tr.reason), tx.NowMs(), c.JobID)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Backoff 返回"全抖动"指数退避的等待时间（执行管理 4.3）：
// Uniform(0, min(上限, 初始间隔 × 2^次数))，以 floor 为下界。抖动用工作标识确定性地派生，便于复现。
func Backoff(seed string, n int, base, ceiling, floor time.Duration) time.Duration {
	if n < 0 {
		n = 0
	}
	if n > 30 {
		n = 30
	}
	upper := base << n
	if upper > ceiling || upper <= 0 {
		upper = ceiling
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(seed))
	r := rand.New(rand.NewPCG(h.Sum64(), uint64(n)))
	w := time.Duration(r.Int64N(int64(upper) + 1))
	if w < floor {
		w = floor
	}
	return w
}

func encodeClaims(cs []Claim) ([]byte, error) { return json.Marshal(cs) }

func decodeClaims(b []byte) ([]Claim, error) {
	var out []Claim
	err := json.Unmarshal(b, &out)
	return out, err
}
