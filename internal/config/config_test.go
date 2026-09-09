package config

import (
	"strings"
	"testing"
	"time"
)

func TestFromEnvReadsTelegramAdminUserID(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("TELEGRAM_ADMIN_USER_ID", " 123456789 ")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TelegramAdminUserID != 123456789 {
		t.Fatalf("TelegramAdminUserID = %d", cfg.TelegramAdminUserID)
	}
}

func TestFromEnvReadsDeduplicationToggle(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("DEDUPLICATION_ENABLED", "false")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DeduplicationEnabled {
		t.Fatal("deduplication remained enabled")
	}
}

func TestFromEnvEnablesDeduplicationByDefault(t *testing.T) {
	setRequiredEnvironment(t)

	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.DeduplicationEnabled {
		t.Fatal("deduplication was not enabled by default")
	}
}

func TestFromEnvRejectsInvalidDeduplicationToggle(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("DEDUPLICATION_ENABLED", "eventually")
	if _, err := FromEnv(); err == nil {
		t.Fatal("invalid deduplication toggle was accepted")
	}
}

func TestFromEnvAllowsDisabledTelegramAdmin(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("TELEGRAM_ADMIN_USER_ID", "")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TelegramAdminUserID != 0 {
		t.Fatalf("TelegramAdminUserID = %d", cfg.TelegramAdminUserID)
	}
}

func TestFromEnvRejectsInvalidTelegramAdminUserID(t *testing.T) {
	for _, value := range []string{"not-a-number", "0", "-42"} {
		t.Run(value, func(t *testing.T) {
			setRequiredEnvironment(t)
			t.Setenv("TELEGRAM_ADMIN_USER_ID", value)

			_, err := FromEnv()
			if err == nil || !strings.Contains(err.Error(), "TELEGRAM_ADMIN_USER_ID") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestFromEnvReadsDeduplicationTuning(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("SIGNAL_WORKERS", "4")
	t.Setenv("SIGNAL_PHOTO_LIMIT", "6")
	t.Setenv("DUPLICATE_WORKERS", "3")
	t.Setenv("DUPLICATE_CANDIDATE_LIMIT", "75")
	t.Setenv("AVAILABILITY_WORKERS", "4")
	t.Setenv("AVAILABILITY_CHECK_INTERVAL_HOURS", "12")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SignalWorkers != 4 || cfg.SignalPhotoLimit != 6 ||
		cfg.DuplicateWorkers != 3 || cfg.DuplicateCandidateLimit != 75 ||
		cfg.AvailabilityWorkers != 4 || cfg.AvailabilityInterval != 12*time.Hour {
		t.Fatalf("unexpected deduplication tuning: %+v", cfg)
	}
}

func TestFromEnvRejectsInvalidDeduplicationTuning(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{"signal-workers-low", "SIGNAL_WORKERS", "0"},
		{"signal-workers-high", "SIGNAL_WORKERS", "9"},
		{"photos-low", "SIGNAL_PHOTO_LIMIT", "0"},
		{"photos-high", "SIGNAL_PHOTO_LIMIT", "9"},
		{"duplicate-workers-low", "DUPLICATE_WORKERS", "0"},
		{"duplicate-workers-high", "DUPLICATE_WORKERS", "9"},
		{"candidates-low", "DUPLICATE_CANDIDATE_LIMIT", "0"},
		{"candidates-high", "DUPLICATE_CANDIDATE_LIMIT", "101"},
		{"availability-workers-low", "AVAILABILITY_WORKERS", "0"},
		{"availability-workers-high", "AVAILABILITY_WORKERS", "9"},
		{"availability-interval-low", "AVAILABILITY_CHECK_INTERVAL_HOURS", "0"},
		{"availability-interval-high", "AVAILABILITY_CHECK_INTERVAL_HOURS", "721"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setRequiredEnvironment(t)
			t.Setenv(test.key, test.value)
			if _, err := FromEnv(); err == nil {
				t.Fatalf("%s=%q was accepted", test.key, test.value)
			}
		})
	}
}

func setRequiredEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
	t.Setenv("SCRAPER_CONTACT", "test@example.com")
	t.Setenv("POLL_INTERVAL_SECONDS", "")
	t.Setenv("DEDUPLICATION_ENABLED", "")
	t.Setenv("SIGNAL_WORKERS", "")
	t.Setenv("SIGNAL_PHOTO_LIMIT", "")
	t.Setenv("DUPLICATE_WORKERS", "")
	t.Setenv("DUPLICATE_CANDIDATE_LIMIT", "")
	t.Setenv("AVAILABILITY_WORKERS", "")
	t.Setenv("AVAILABILITY_CHECK_INTERVAL_HOURS", "")
}
