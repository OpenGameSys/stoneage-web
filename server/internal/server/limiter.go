package server

import (
	"sync"
	"time"
)

// authLimiter 是登录 / 注册的限速：按来源 IP 与账号名各记连续失败次数，失败 authFailLimit 次后锁住，
// 每多锁一次时间加倍（authLockBase、authLockMax）；成功一次就清掉。另外用一个信号量限制同时在算的密码哈希数
// （PBKDF2 一次要几十毫秒，不限的话一条连线就能占满一个核）。
type authLimiter struct {
	mu      sync.Mutex
	entries map[string]*authEntry
	hash    chan struct{}
	now     func() time.Time
}

type authEntry struct {
	fails   int
	locks   int       // 锁过几次（决定下一次锁多久）
	until   time.Time // 锁到什么时候
	touched time.Time
}

const (
	authFailLimit  = 5
	authLockBase   = time.Minute
	authLockMax    = time.Hour
	authForget     = 24 * time.Hour // 这么久没动静的记录清掉
	authHashAtOnce = 4              // 同时最多几条连线在算哈希
	authRegPerHour = 5              // 一个 IP 一小时最多注册几个账号
)

func newAuthLimiter() *authLimiter {
	return &authLimiter{entries: map[string]*authEntry{}, hash: make(chan struct{}, authHashAtOnce), now: time.Now}
}

func (l *authLimiter) get(key string) *authEntry {
	e := l.entries[key]
	if e == nil {
		e = &authEntry{}
		l.entries[key] = e
		if len(l.entries) > 4096 {
			l.sweep()
		}
	}
	e.touched = l.now()
	return e
}

func (l *authLimiter) sweep() {
	cut := l.now().Add(-authForget)
	for k, e := range l.entries {
		if e.touched.Before(cut) && e.until.Before(l.now()) {
			delete(l.entries, k)
		}
	}
}

// locked 回传 key 还要锁多久（0 表示没锁）。
func (l *authLimiter) locked(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e := l.entries[key]; e != nil && e.until.After(l.now()) {
		return e.until.Sub(l.now())
	}
	return 0
}

// fail 记一次失败；到次数就锁住并回传锁多久。
func (l *authLimiter) fail(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.get(key)
	e.fails++
	if e.fails < authFailLimit {
		return 0
	}
	d := min(authLockMax, authLockBase<<e.locks)
	e.locks++
	e.fails = 0
	e.until = l.now().Add(d)
	return d
}

// ok 是成功了：清掉失败次数（锁次数保留一天，连续猜对一次再猜错不会从头算）。
func (l *authLimiter) ok(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e := l.entries[key]; e != nil {
		e.fails = 0
	}
}

// count 是注册这类"每小时最多 n 次"的计数：回传这一小时内第几次（含这次）。
func (l *authLimiter) count(key string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.get(key)
	if e.until.Before(l.now()) { // 用 until 当作这一小时的结束
		e.fails = 0
		e.until = l.now().Add(time.Hour)
	}
	e.fails++
	return e.fails
}

// acquireHash 拿一个算哈希的名额（最多等 ctxDone）。
func (l *authLimiter) acquireHash(done <-chan struct{}) bool {
	select {
	case l.hash <- struct{}{}:
		return true
	case <-done:
		return false
	}
}

func (l *authLimiter) releaseHash() { <-l.hash }
