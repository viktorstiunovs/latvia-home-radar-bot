package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	TelegramBotToken    string
	TelegramAdminUserID int64
	ScraperContact      string
	DatabaseURL         string
	RabbitMQURL         string
	PollInterval        time.Duration
	LogLevel            string
}

func FromEnv() (Config, error) {
	c := Config{
		TelegramBotToken: strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		ScraperContact:   strings.TrimSpace(os.Getenv("SCRAPER_CONTACT")),
		DatabaseURL:      strings.TrimSpace(os.Getenv("DATABASE_URL")),
		RabbitMQURL:      strings.TrimSpace(os.Getenv("RABBITMQ_URL")),
		LogLevel:         strings.ToUpper(strings.TrimSpace(os.Getenv("LOG_LEVEL"))),
	}
	if c.DatabaseURL == "" {
		c.DatabaseURL = "postgresql://apartment:apartment@localhost:5432/apartment_alerts"
	}
	if c.RabbitMQURL == "" {
		c.RabbitMQURL = "amqp://radar:radar@localhost:5672/"
	}
	if c.LogLevel == "" {
		c.LogLevel = "INFO"
	}
	if c.TelegramBotToken == "" {
		return c, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	if c.ScraperContact == "" {
		return c, fmt.Errorf("SCRAPER_CONTACT is required")
	}
	if raw := strings.TrimSpace(os.Getenv("TELEGRAM_ADMIN_USER_ID")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return c, fmt.Errorf("TELEGRAM_ADMIN_USER_ID: %w", err)
		}
		if value <= 0 {
			return c, fmt.Errorf("TELEGRAM_ADMIN_USER_ID must be positive")
		}
		c.TelegramAdminUserID = value
	}
	seconds := 180
	if raw := strings.TrimSpace(os.Getenv("POLL_INTERVAL_SECONDS")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return c, fmt.Errorf("POLL_INTERVAL_SECONDS: %w", err)
		}
		seconds = value
	}
	if seconds < 30 {
		return c, fmt.Errorf("POLL_INTERVAL_SECONDS must be at least 30")
	}
	c.PollInterval = time.Duration(seconds) * time.Second
	return c, nil
}

func (c Config) UserAgent() string {
	return "LatviaHomeRadar/1.0 (+" + c.ScraperContact + ")"
}
