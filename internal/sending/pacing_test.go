package sending

import (
	"testing"
	"time"
)

func TestPacingLoweringRateRetainsRollingHistory(t *testing.T) {
	now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	s := Settings{Rate: 30}
	for n := range 30 {
		s.Starts = append(s.Starts, now.Add(time.Duration(n)*2*time.Second))
	}
	// Lowering to one must wait until the newest actual start leaves the window.
	s.Rate = 1
	if got, want := nextStart(now.Add(59*time.Second), s), now.Add(118*time.Second); !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestPacingRestartCooldownAndDefault(t *testing.T) {
	now := time.Now().UTC()
	for _, n := range []int{1, 25, 30} {
		s := Settings{Rate: n, Starts: []time.Time{now}, NextEligible: now.Add(time.Minute / time.Duration(n))}
		if got := nextStart(now, s); !got.Equal(s.NextEligible) {
			t.Fatalf("rate %d: %v", n, got)
		}
		s.CooldownUntil = now.Add(90 * time.Second)
		if got := nextStart(now, s); !got.Equal(s.CooldownUntil) {
			t.Fatalf("cooldown lost: %v", got)
		}
	}
	if got := nextStart(now, Settings{Starts: []time.Time{now}}); !got.Equal(now.Add(time.Minute / 25)) {
		t.Fatal(got)
	}
}

func TestPacingNoRollingMinuteExceedsRate(t *testing.T) {
	for _, n := range []int{1, 25, 30} {
		now := time.Now().UTC()
		s := Settings{Rate: n}
		for i := range 1000 {
			start := nextStart(now, s)
			count := 0
			for _, old := range s.Starts {
				if old.After(start.Add(-time.Minute)) {
					count++
				}
			}
			if count >= n {
				t.Fatalf("rate %d exceeded at %d: %d", n, i, count)
			}
			s.Starts = append(s.Starts, start)
			now = start
		}
	}
}

func TestRateValidation(t *testing.T) {
	for n := -1; n <= 31; n++ {
		if (ValidateRate(n) == nil) != (n >= 1 && n <= 30) {
			t.Fatal(n)
		}
	}
}

func TestOwnershipExclusiveAndReleased(t *testing.T) {
	dir := t.TempDir()
	a, err := acquireOwnership(dir)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := acquireOwnership(dir); err == nil {
		if err := b.Close(); err != nil {
			t.Fatal(err)
		}
		t.Fatal("overlapping owner accepted")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := acquireOwnership(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := b.Close(); err != nil {
			t.Error(err)
		}
	}()
}
