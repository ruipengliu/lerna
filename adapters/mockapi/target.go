// Package mockapi 是可注入故障的模拟 API（执行适配器接口 9、项目目标 V3）：
// 幂等、可查询、不可查询三类目标，以及按这三类如实声明能力的参考适配器。
//
// 模拟目标独立于被测进程记录真实收到的请求、外部键和生效时间：harness 崩溃重启后，
// 测试仍用它判定是否重复发送。
package mockapi

import (
	"fmt"
	"sync"
	"time"
)

// Class 是模拟目标的能力类别。
type Class int

const (
	// Idempotent 对同一外部键幂等（在键的有效期内），不支持查询。
	Idempotent Class = iota + 1
	// Queryable 可以按尝试标识查询结果，不幂等。
	Queryable
	// Unqueryable 既不可查询也不幂等。
	Unqueryable
)

func (c Class) String() string {
	switch c {
	case Idempotent:
		return "idempotent"
	case Queryable:
		return "queryable"
	case Unqueryable:
		return "unqueryable"
	}
	return "unknown"
}

// Fault 是注入到某一次请求上的故障。
type Fault int

const (
	// None 正常处理。
	None Fault = iota
	// LoseBeforeReceive：请求没有到达目标（目标侧没有任何记录）。
	LoseBeforeReceive
	// LoseAfterAccept：目标接单，效果在 EffectDelay 之后生效，回执丢失。
	LoseAfterAccept
	// LoseAfterCommit：效果已生效，回执丢失。
	LoseAfterCommit
	// DelayedEffect：目标返回"已接受"（202），效果在 EffectDelay 之后生效。
	DelayedEffect
	// RateLimit：目标返回 429，不产生效果。
	RateLimit
	// Reject：目标明确拒绝（例如参数校验失败），不产生效果，结果是终局的。
	Reject
)

// Call 是目标实际收到的一次请求。
type Call struct {
	Seq         int
	RequestID   string
	ExternalKey string
	AttemptID   string
	Key         string
	Value       string
	At          time.Time
	// Deduplicated 表示命中幂等键，没有产生新效果。
	Deduplicated bool
	Fault        Fault
	Query        bool
}

type effect struct {
	attemptID string
	key, val  string
	at        time.Time
	requestID string
	cancelled bool
}

type idemEntry struct {
	requestID string
	expires   time.Time
	attemptID string
	key, val  string
}

// Target 是模拟 API 的服务端。它在测试进程中独立于被测宿主存在。
type Target struct {
	mu    sync.Mutex
	class Class
	now   func() time.Time

	calls   []Call
	state   map[string]string
	applied []effect
	pending []effect
	idem    map[string]idemEntry
	faults  map[int]Fault
	seq     int

	// EffectDelay 是接单后效果延迟生效的时间。
	EffectDelay time.Duration
	// QueryLag 是查询的可见性延迟：生效后这段时间内查询返回"查无记录"（弱查询）。
	QueryLag time.Duration
	// KeyTTL 是幂等键的保存期；之后同一个键会被当作新请求。
	KeyTTL time.Duration
	// Price 是每个实际收到的请求的费用（micro_usd）。
	Price int64
	// RateLimitKind 是注入 429 时声明的限流类别。
	RateLimitKind string
}

// NewTarget 创建一个模拟目标。now 是目标侧的时钟（通常与 harness 共用）。
func NewTarget(class Class, now func() time.Time) *Target {
	return &Target{
		class:  class,
		now:    now,
		state:  map[string]string{},
		idem:   map[string]idemEntry{},
		faults: map[int]Fault{},
		KeyTTL: 24 * time.Hour,
		Price:  100,
	}
}

// Class 返回目标类别。
func (t *Target) Class() Class { return t.class }

// InjectOnCall 让第 n 次收到的执行请求（从 1 开始，含被丢失的）注入故障。
func (t *Target) InjectOnCall(n int, f Fault) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.faults[n] = f
}

// advance 让到期的延迟效果生效。
func (t *Target) advance() {
	now := t.now()
	var keep []effect
	for _, e := range t.pending {
		if e.cancelled {
			continue
		}
		if !now.Before(e.at) {
			t.state[e.key] = e.val
			e.at = now
			t.applied = append(t.applied, e)
		} else {
			keep = append(keep, e)
		}
	}
	t.pending = keep
}

// Response 是目标对一次请求的回应；Lost 为真表示回应没有送回调用方。
type Response struct {
	Lost       bool
	Status     int
	RequestID  string
	Committed  bool
	Accepted   bool
	Dedup      bool
	RetryAfter time.Duration
	Cost       int64
}

// Put 处理一次执行请求：把 key 设为 value。externalKey 是调用方显式传入的幂等键；
// deadline 非零时，期限之后到达的请求被拒绝，不产生效果（目标声明的迟到终局语义）。
func (t *Target) Put(externalKey, attemptID, key, value string, deadline time.Time) Response {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.advance()
	t.seq++
	n := t.seq
	f := t.faults[n]
	if f == LoseBeforeReceive {
		return Response{Lost: true}
	}
	now := t.now()
	if !deadline.IsZero() && now.After(deadline) {
		t.calls = append(t.calls, Call{Seq: n, RequestID: fmt.Sprintf("req-%d", n), ExternalKey: externalKey, AttemptID: attemptID,
			Key: key, Value: value, At: now, Fault: Reject})
		return Response{Status: 422, RequestID: fmt.Sprintf("req-%d", n)}
	}
	reqID := fmt.Sprintf("req-%d", n)
	call := Call{Seq: n, RequestID: reqID, ExternalKey: externalKey, AttemptID: attemptID, Key: key, Value: value, At: now, Fault: f}
	if t.class == Idempotent && externalKey != "" {
		if e, ok := t.idem[externalKey]; ok && now.Before(e.expires) {
			call.Deduplicated = true
			t.calls = append(t.calls, call)
			_, committed := t.appliedFor(e.attemptID)
			return Response{Status: 200, RequestID: e.requestID, Committed: committed, Accepted: !committed, Dedup: true, Lost: f == LoseAfterCommit || f == LoseAfterAccept}
		}
	}
	t.calls = append(t.calls, call)
	switch f {
	case RateLimit:
		return Response{Status: 429, RequestID: reqID, RetryAfter: time.Second}
	case Reject:
		return Response{Status: 422, RequestID: reqID, Cost: t.Price}
	}
	e := effect{attemptID: attemptID, key: key, val: value, at: now, requestID: reqID}
	if t.class == Idempotent && externalKey != "" {
		t.idem[externalKey] = idemEntry{requestID: reqID, expires: now.Add(t.KeyTTL), attemptID: attemptID, key: key, val: value}
	}
	switch f {
	case LoseAfterAccept, DelayedEffect:
		e.at = now.Add(t.EffectDelay)
		t.pending = append(t.pending, e)
		t.advance()
		if f == LoseAfterAccept {
			return Response{Lost: true, RequestID: reqID}
		}
		return Response{Status: 202, RequestID: reqID, Accepted: true, Cost: t.Price}
	default:
		t.state[key] = value
		t.applied = append(t.applied, e)
		if f == LoseAfterCommit {
			return Response{Lost: true, RequestID: reqID}
		}
		return Response{Status: 200, RequestID: reqID, Committed: true, Cost: t.Price}
	}
}

func (t *Target) appliedFor(attemptID string) (effect, bool) {
	for _, e := range t.applied {
		if e.attemptID == attemptID {
			return e, true
		}
	}
	return effect{}, false
}

// QueryResult 是目标对查询的回应。
type QueryResult struct {
	Supported bool
	Found     bool
	Committed bool
	// Pending 表示请求已被目标接单、尚未生效。
	Pending bool
	// Terminal 表示目标确认这个尝试不会再生效（例如接单记录已终止）。
	Terminal  bool
	RequestID string
	Value     string
}

// Query 按尝试标识查询结果。只有可查询目标支持；查询不重新执行原动作。
// notAfter 是原尝试最晚的请求期限：期限和可见性延迟都过去之后仍查不到，才是终局的"未生效"。
func (t *Target) Query(attemptID string, notAfter time.Time) QueryResult {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.advance()
	t.calls = append(t.calls, Call{Seq: 0, AttemptID: attemptID, At: t.now(), Query: true})
	if t.class != Queryable {
		return QueryResult{}
	}
	now := t.now()
	if e, ok := t.appliedFor(attemptID); ok {
		if now.Before(e.at.Add(t.QueryLag)) {
			// 弱查询：已生效，但暂时查不到。
			return QueryResult{Supported: true}
		}
		return QueryResult{Supported: true, Found: true, Committed: true, Terminal: true, RequestID: e.requestID, Value: e.val}
	}
	for _, e := range t.pending {
		if e.attemptID == attemptID {
			if now.Before(e.at.Add(-t.EffectDelay).Add(t.QueryLag)) {
				return QueryResult{Supported: true}
			}
			return QueryResult{Supported: true, Found: true, Pending: true, RequestID: e.requestID}
		}
	}
	for _, c := range t.calls {
		if c.AttemptID == attemptID && !c.Query && (c.Fault == RateLimit || c.Fault == Reject) {
			return QueryResult{Supported: true, Found: true, Terminal: true, RequestID: c.RequestID}
		}
	}
	if !notAfter.IsZero() && !now.Before(notAfter.Add(t.QueryLag)) {
		// 期限之后到达的请求一律拒绝，而可见性延迟也已过去：原请求不会再生效。
		return QueryResult{Supported: true, Terminal: true}
	}
	return QueryResult{Supported: true}
}

// Received 返回目标实际收到的执行请求次数（不含查询）。
func (t *Target) Received() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for _, c := range t.calls {
		if !c.Query {
			n++
		}
	}
	return n
}

// Queries 返回目标收到的查询次数。
func (t *Target) Queries() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for _, c := range t.calls {
		if c.Query {
			n++
		}
	}
	return n
}

// Applied 返回实际产生效果的次数（含迟到生效）。
func (t *Target) Applied() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.advance()
	return len(t.applied)
}

// Calls 返回实际收到的请求。
func (t *Target) Calls() []Call {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]Call(nil), t.calls...)
}

// Value 返回资源当前值。
func (t *Target) Value(key string) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.advance()
	v, ok := t.state[key]
	return v, ok
}

// Settle 让到期的延迟效果生效（测试推进时钟后调用）。
func (t *Target) Settle() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.advance()
}
