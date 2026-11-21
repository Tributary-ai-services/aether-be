package services

import (
	"testing"
	"time"
)

func TestValidateJoinedAt(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		at time.Time
		ok bool
	}{
		{now.AddDate(0, -2, 0), true},     // back-dated
		{now, true},                       // now
		{now.Add(30 * time.Second), true}, // clock skew
		{now.Add(2 * time.Minute), false}, // future
	} {
		if err := ValidateJoinedAt(c.at, now); (err == nil) != c.ok {
			t.Errorf("ValidateJoinedAt(%v) = %v, want ok=%v", c.at, err, c.ok)
		}
	}
}
