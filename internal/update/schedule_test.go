package update

import (
	"testing"
	"time"
)

func TestDueForAutoCheck(t *testing.T) {
	now := time.Date(2026, 3, 24, 12, 0, 0, 0, time.UTC)

	if !DueForAutoCheck("", now) {
		t.Fatal("empty last check should be due")
	}
	if !DueForAutoCheck("not-a-time", now) {
		t.Fatal("invalid last check should be due")
	}

	last := now.Add(-AutoCheckMinInterval).Format(time.RFC3339)
	if !DueForAutoCheck(last, now) {
		t.Fatal("check exactly one interval ago should be due")
	}

	recent := now.Add(-AutoCheckMinInterval + time.Minute).Format(time.RFC3339)
	if DueForAutoCheck(recent, now) {
		t.Fatal("check inside interval should not be due")
	}
}
