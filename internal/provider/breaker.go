// Package provider 维护供应商熔断器与运行指标。
package provider

import (
	"sync"
	"time"
)

// State 是熔断器状态。
type State string

const (
	StateClosed   State = "closed"
	StateOpen     State = "open"
	StateHalfOpen State = "half-open"
)

// Config 是熔断参数。
type Config struct {
	FailureThreshold int           // 连续失败阈值（默认 3）
	Cooldown         time.Duration // 初始冷却（默认 60s）
	MaxCooldown      time.Duration // 冷却上限（默认 15m）
}

// DefaultConfig 返回默认参数。
func DefaultConfig() Config {
	return Config{FailureThreshold: 3, Cooldown: 60 * time.Second, MaxCooldown: 15 * time.Minute}
}

// Breaker 是单供应商被动熔断器：连续失败达阈值打开，
// 冷却指数退避后半开放行单探针（真实请求兼作探针）。
type Breaker struct {
	mu       sync.Mutex
	cfg      Config
	now      func() time.Time
	state    State
	fails    int
	openedAt time.Time
	cooldown time.Duration
	probing  bool // 半开探针在途
}

// NewBreaker 构造熔断器；now 为 nil 用真实时钟（测试注入）。
func NewBreaker(cfg Config, now func() time.Time) *Breaker {
	if cfg.FailureThreshold <= 0 || cfg.Cooldown <= 0 || cfg.MaxCooldown <= 0 {
		def := DefaultConfig()
		if cfg.FailureThreshold <= 0 {
			cfg.FailureThreshold = def.FailureThreshold
		}
		if cfg.Cooldown <= 0 {
			cfg.Cooldown = def.Cooldown
		}
		if cfg.MaxCooldown <= 0 {
			cfg.MaxCooldown = def.MaxCooldown
		}
	}
	if now == nil {
		now = time.Now
	}
	return &Breaker{cfg: cfg, now: now, state: StateClosed}
}

// Allow 报告是否放行请求。
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case StateClosed:
		return true
	case StateOpen:
		if b.now().Sub(b.openedAt) >= b.cooldown {
			b.state = StateHalfOpen
			b.probing = true
			return true
		}
		return false
	case StateHalfOpen:
		if b.probing {
			return false
		}
		b.probing = true
		return true
	}
	return true
}

// RecordSuccess 记录成功：关闭熔断并清零计数。
func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fails = 0
	b.cooldown = 0
	b.probing = false
	b.state = StateClosed
}

// RecordFailure 记录失败：累计并按状态转移。
func (b *Breaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fails++
	switch b.state {
	case StateHalfOpen:
		// 探针失败：重开并指数退避
		b.reopen()
	case StateClosed:
		if b.fails >= b.cfg.FailureThreshold {
			b.reopen()
		}
	case StateOpen:
		// 打开期间的失败（在途请求）延长冷却
		b.reopen()
	}
}

// reopen 计算冷却并打开。
func (b *Breaker) reopen() {
	if b.cooldown == 0 {
		b.cooldown = b.cfg.Cooldown
	} else {
		b.cooldown *= 2
		if b.cooldown > b.cfg.MaxCooldown {
			b.cooldown = b.cfg.MaxCooldown
		}
	}
	b.state = StateOpen
	b.openedAt = b.now()
	b.probing = false
}

// State 返回当前状态。
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// ---- 注册表：熔断器 + 指标 ----

// SnapshotEntry 是 status 展示用的快照。
type SnapshotEntry struct {
	Name     string
	State    State
	Requests int64
	Failures int64
	InFlight int64
	LastErr  string
	LastEnd  time.Time
}

type entry struct {
	breaker   *Breaker
	requests  int64
	failures  int64
	inFlight  int64
	lastErr   string
	lastEnd   time.Time
	lastEndOK bool
}

// Registry 聚合各供应商的熔断器与计数。
type Registry struct {
	mu      sync.Mutex
	entries map[string]*entry
}

// NewRegistry 构造注册表。
func NewRegistry() *Registry {
	return &Registry{entries: map[string]*entry{}}
}

// Upsert 注册或更新供应商配置（保留计数与熔断状态）。
func (r *Registry) Upsert(name string, cfg Config, now func() time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[name]
	if !ok {
		e = &entry{}
		r.entries[name] = e
	}
	e.breaker = NewBreaker(cfg, now)
}

// Remove 移除供应商。
func (r *Registry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, name)
}

func (r *Registry) get(name string) *entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.entries[name]
}

// Breaker 返回供应商熔断器。
func (r *Registry) Breaker(name string) (*Breaker, bool) {
	e := r.get(name)
	if e == nil {
		return nil, false
	}
	return e.breaker, true
}

// RecordRequest 记录一次请求开始（在途 +1）。
func (r *Registry) RecordRequest(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e := r.entries[name]; e != nil {
		e.requests++
		e.inFlight++
	}
}

// RecordSuccess 记录成功（在途 -1，熔断关闭）。
func (r *Registry) RecordSuccess(name string) {
	r.mu.Lock()
	e := r.entries[name]
	r.mu.Unlock()
	if e == nil {
		return
	}
	e.breaker.RecordSuccess()
	r.mu.Lock()
	e.inFlight--
	e.lastEnd = time.Now()
	e.lastEndOK = true
	r.mu.Unlock()
}

// RecordFailure 记录失败（在途 -1，熔断累计）。
func (r *Registry) RecordFailure(name string, errText string) {
	r.mu.Lock()
	e := r.entries[name]
	r.mu.Unlock()
	if e == nil {
		return
	}
	e.breaker.RecordFailure()
	r.mu.Lock()
	e.failures++
	e.inFlight--
	e.lastErr = errText
	e.lastEnd = time.Now()
	e.lastEndOK = false
	r.mu.Unlock()
}

// Snapshot 返回全部供应商快照（按名排序）。
func (r *Registry) Snapshot() []SnapshotEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]SnapshotEntry, 0, len(r.entries))
	for name, e := range r.entries {
		var state State = StateClosed
		var lastErr string
		if e.breaker != nil {
			state = e.breaker.State()
		}
		if !e.lastEndOK {
			lastErr = e.lastErr
		}
		out = append(out, SnapshotEntry{
			Name: name, State: state,
			Requests: e.requests, Failures: e.failures, InFlight: e.inFlight,
			LastErr: lastErr, LastEnd: e.lastEnd,
		})
	}
	sortEntries(out)
	return out
}

func sortEntries(s []SnapshotEntry) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].Name < s[j-1].Name; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
