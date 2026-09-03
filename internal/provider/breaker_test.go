package provider

import (
	"testing"
	"time"
)

func TestBreakerOpensAfterThreshold(t *testing.T) {
	now := time.Unix(1000, 0)
	b := NewBreaker(Config{FailureThreshold: 3, Cooldown: 60 * time.Second, MaxCooldown: 15 * time.Minute}, func() time.Time { return now })
	for i := 0; i < 3; i++ {
		if !b.Allow() {
			t.Fatalf("第 %d 次应放行", i+1)
		}
		b.RecordFailure()
	}
	if b.Allow() {
		t.Fatal("连续 3 败后应打开熔断")
	}
	if b.State() != StateOpen {
		t.Errorf("state = %v", b.State())
	}
}

func TestBreakerHalfOpenProbe(t *testing.T) {
	now := time.Unix(1000, 0)
	b := NewBreaker(Config{FailureThreshold: 1, Cooldown: 60 * time.Second, MaxCooldown: 15 * time.Minute}, func() time.Time { return now })
	b.RecordFailure() // 1 败即开

	// 冷却期内拒绝
	if b.Allow() {
		t.Fatal("冷却期内应拒绝")
	}
	// 冷却后放行一个探针
	now = now.Add(61 * time.Second)
	if !b.Allow() {
		t.Fatal("冷却后应放行半开探针")
	}
	if b.Allow() {
		t.Fatal("半开只放行一个探针")
	}
	// 探针成功 → 关闭
	b.RecordSuccess()
	if b.State() != StateClosed {
		t.Fatalf("探针成功应关闭，state=%v", b.State())
	}
	if !b.Allow() {
		t.Fatal("关闭后应正常放行")
	}
}

func TestBreakerHalfOpenProbeFailureReopens(t *testing.T) {
	now := time.Unix(1000, 0)
	b := NewBreaker(Config{FailureThreshold: 1, Cooldown: 60 * time.Second, MaxCooldown: 15 * time.Minute}, func() time.Time { return now })
	b.RecordFailure()
	now = now.Add(61 * time.Second)
	if !b.Allow() {
		t.Fatal("半开探针应放行")
	}
	b.RecordFailure() // 探针失败 → 重开，冷却翻倍
	if b.State() != StateOpen {
		t.Fatalf("探针失败应重开，state=%v", b.State())
	}
	now = now.Add(61 * time.Second) // 原冷却已不够（翻倍为 120s）
	if b.Allow() {
		t.Fatal("指数冷却期内应继续拒绝")
	}
	now = now.Add(70 * time.Second)
	if !b.Allow() {
		t.Fatal("冷却到期后应放行")
	}
}

func TestBreakerSuccessResetsCount(t *testing.T) {
	b := NewBreaker(Config{FailureThreshold: 3, Cooldown: 60 * time.Second, MaxCooldown: 15 * time.Minute}, nil)
	b.RecordFailure()
	b.RecordFailure()
	b.RecordSuccess()
	b.RecordFailure()
	b.RecordFailure()
	if b.State() != StateClosed {
		t.Fatalf("成功应清零连续失败计数，state=%v", b.State())
	}
}

func TestRegistrySnapshotAndCounters(t *testing.T) {
	r := NewRegistry()
	now := time.Unix(0, 0)
	r.Upsert("a", Config{FailureThreshold: 2, Cooldown: time.Second, MaxCooldown: time.Second}, func() time.Time { return now })

	// 计数
	r.RecordRequest("a")
	r.RecordRequest("a")
	r.RecordFailure("a", "boom")
	snap := r.Snapshot()
	if len(snap) != 1 || snap[0].Name != "a" {
		t.Fatalf("snapshot = %+v", snap)
	}
	if snap[0].Requests != 2 || snap[0].Failures != 1 {
		t.Errorf("counters = %+v", snap[0])
	}
	if snap[0].State != StateClosed {
		t.Errorf("state = %v", snap[0].State)
	}
	// 未知供应商不 panic
	r.RecordFailure("ghost", "x")
	if _, ok := r.Breaker("ghost"); ok {
		t.Error("未知供应商不应返回 breaker")
	}
}
