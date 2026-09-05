package config

import (
	"strings"
	"testing"
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

func setRequiredEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
	t.Setenv("SCRAPER_CONTACT", "test@example.com")
	t.Setenv("POLL_INTERVAL_SECONDS", "")
}
