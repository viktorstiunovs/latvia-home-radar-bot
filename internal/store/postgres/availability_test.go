package postgres

import (
	"testing"
	"time"
)

func TestAvailabilityUnknownDelayGrowsAndCaps(t *testing.T) {
	base := 24 * time.Hour
	if actual := availabilityUnknownDelay(base, 0); actual != base {
		t.Fatalf("first delay=%v", actual)
	}
	if actual := availabilityUnknownDelay(base, 3); actual != 8*base {
		t.Fatalf("fourth delay=%v", actual)
	}
	if actual := availabilityUnknownDelay(base, 20); actual != 30*24*time.Hour {
		t.Fatalf("capped delay=%v", actual)
	}
}

func TestAvailabilityActiveDelayIsAtLeastWeekly(t *testing.T) {
	if actual := availabilityActiveDelay(24 * time.Hour); actual != 7*24*time.Hour {
		t.Fatalf("daily stale window active delay=%v", actual)
	}
	if actual := availabilityActiveDelay(14 * 24 * time.Hour); actual != 14*24*time.Hour {
		t.Fatalf("long stale window active delay=%v", actual)
	}
}
