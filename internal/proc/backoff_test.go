package proc

import (
	"testing"
	"time"
)

func TestBackoffCapsResetsAndDegrades(t *testing.T) {
	backoff := &Backoff{Initial: time.Second, Max: 4 * time.Second, ResetAfter: time.Minute, GiveUp: 4}
	now := time.Unix(1000, 0)
	for i, want := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		delay, degraded := backoff.Failure(now.Add(time.Duration(i)*time.Second), 10*time.Second)
		if delay != want || degraded {
			t.Fatalf("failure %d = %s, %v", i, delay, degraded)
		}
	}
	delay, degraded := backoff.Failure(now.Add(3*time.Second), 10*time.Second)
	if delay != 4*time.Second || !degraded {
		t.Fatalf("give up = %s, %v", delay, degraded)
	}

	backoff.Reset()
	delay, degraded = backoff.Failure(now.Add(20*time.Minute), 2*time.Minute)
	if delay != time.Second || degraded {
		t.Fatalf("stable reset = %s, %v", delay, degraded)
	}
}
