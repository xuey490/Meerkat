package loginguard

import (
	"context"
	"testing"
	"time"

	"github.com/company/monitor-webserver/internal/cachestore"
)

// newTestGuard 构造一个使用进程内缓存、锁定 100ms 的守卫。
func newTestGuard(limit int, lockIP bool) *Guard {
	lockout := 100 * time.Millisecond
	return New(cachestore.New(nil, "test:", lockout), Options{
		Limit:   limit,
		Lockout: lockout,
		LockIP:  lockIP,
	})
}

func TestFailureLocksAccountAtThreshold(t *testing.T) {
	ctx := context.Background()
	guard := newTestGuard(3, false)

	for i := 1; i <= 2; i++ {
		status := guard.Failure(ctx, "admin", "10.0.0.1")
		if status.Locked {
			t.Fatalf("第 %d 次失败不应锁定，got %+v", i, status)
		}
		if status.Attempts != i || status.RemainingAttempts() != 3-i {
			t.Fatalf("第 %d 次失败计数不正确: %+v", i, status)
		}
	}

	status := guard.Failure(ctx, "admin", "10.0.0.1")
	if !status.Locked || status.Scope != ScopeAccount {
		t.Fatalf("达到阈值后应按账号锁定: %+v", status)
	}
	if status.RetryAfter <= 0 {
		t.Fatalf("锁定时应返回剩余解锁时间: %+v", status)
	}
	if inspect := guard.Inspect(ctx, "admin", "10.0.0.1"); !inspect.Locked {
		t.Fatalf("Inspect 应识别出锁定: %+v", inspect)
	}
}

func TestLockoutExpiresAutomatically(t *testing.T) {
	ctx := context.Background()
	guard := newTestGuard(1, false)

	if status := guard.Failure(ctx, "admin", "10.0.0.1"); !status.Locked {
		t.Fatalf("阈值 1 应立即锁定: %+v", status)
	}
	time.Sleep(150 * time.Millisecond)
	if status := guard.Inspect(ctx, "admin", "10.0.0.1"); status.Locked {
		t.Fatalf("TTL 到期后应自动解锁: %+v", status)
	}
}

func TestAccountKeyIsCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	guard := newTestGuard(2, false)

	guard.Failure(ctx, "Admin", "10.0.0.1")
	if status := guard.Failure(ctx, " admin ", "10.0.0.1"); !status.Locked {
		t.Fatalf("大小写与空格差异不应绕过账号计数: %+v", status)
	}
}

func TestLockIPStopsUsernameRotation(t *testing.T) {
	ctx := context.Background()
	guard := newTestGuard(2, true)

	guard.Failure(ctx, "user-a", "10.0.0.9")
	status := guard.Failure(ctx, "user-b", "10.0.0.9")
	if !status.Locked || status.Scope != ScopeIP {
		t.Fatalf("同一 IP 轮换账号名也应在达到阈值后锁定来源 IP: %+v", status)
	}
}

func TestLockIPDisabledOnlyLocksAccount(t *testing.T) {
	ctx := context.Background()
	guard := newTestGuard(1, false)

	guard.Failure(ctx, "user-a", "10.0.0.9")
	status := guard.Inspect(ctx, "user-b", "10.0.0.9")
	if status.Locked {
		t.Fatalf("关闭 lockout_ip 后不应因 IP 锁定其他账号: %+v", status)
	}
}

func TestResetClearsAccountButNotIP(t *testing.T) {
	ctx := context.Background()
	guard := newTestGuard(5, true)

	guard.Failure(ctx, "admin", "10.0.0.1")
	guard.Reset(ctx, "admin")
	if status := guard.Inspect(ctx, "admin", "10.0.0.2"); status.Attempts != 0 {
		t.Fatalf("成功登录后应清零账号计数: %+v", status)
	}
	// IP 计数保留：否则用已知账号「错 N 次 + 对 1 次」即可反复重置，绕过 IP 锁定。
	if status := guard.Inspect(ctx, "other", "10.0.0.1"); status.Attempts != 1 {
		t.Fatalf("IP 维度计数不应被成功登录清零: %+v", status)
	}
}

func TestDisabledGuardNeverLocks(t *testing.T) {
	ctx := context.Background()
	guard := newTestGuard(0, true)

	for i := 0; i < 10; i++ {
		if status := guard.Failure(ctx, "admin", "10.0.0.1"); status.Locked {
			t.Fatalf("未启用锁定时不应锁定: %+v", status)
		}
	}
}
