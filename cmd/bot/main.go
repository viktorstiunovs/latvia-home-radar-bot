package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/app"
	"github.com/clive00lewis/latvia-home-radar/internal/broker/rabbitmq"
	"github.com/clive00lewis/latvia-home-radar/internal/config"
	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	"github.com/clive00lewis/latvia-home-radar/internal/localization"
	"github.com/clive00lewis/latvia-home-radar/internal/provider"
	"github.com/clive00lewis/latvia-home-radar/internal/provider/city24"
	"github.com/clive00lewis/latvia-home-radar/internal/provider/sslv"
	"github.com/clive00lewis/latvia-home-radar/internal/store/postgres"
	"github.com/clive00lewis/latvia-home-radar/internal/telegram"
)

func main() {
	cfg, err := config.FromEnv()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(2)
	}
	logger := configureLogger(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg, logger); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func configureLogger(cfg config.Config) *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToUpper(cfg.LogLevel) {
	case "DEBUG":
		level = slog.LevelDebug
	case "WARN", "WARNING":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)
	return logger
}

func run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	store, err := postgres.Open(ctx, cfg.DatabaseURL, 30)
	if err != nil {
		return err
	}
	defer store.Close()

	broker, err := rabbitmq.Open(ctx, cfg.RabbitMQURL, 30)
	if err != nil {
		return err
	}
	defer broker.Close()

	transport := contactTransport{base: http.DefaultTransport, agent: cfg.UserAgent()}
	httpClient := &http.Client{Timeout: 30 * time.Second, Transport: transport}
	api := telegram.NewClient(cfg.TelegramBotToken, httpClient)
	catalog, err := localization.New()
	if err != nil {
		return err
	}
	if err := catalog.Validate(); err != nil {
		return err
	}
	tgBot, err := telegram.NewBot(cfg.TelegramBotToken, cfg.TelegramAdminUserID, api, store, catalog, logger)
	if err != nil {
		return err
	}

	sources := buildSources(httpClient)
	monitor := app.NewMonitor(store, sources, cfg.PollInterval, logger)
	outbox := app.NewOutboxRelay(store, broker, logger)
	matcher := app.NewListingMatcher(store, broker, logger)
	notifier := app.NewNotifier(store, api, httpClient, catalog, logger)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan error, 5)
	go func() { results <- tgBot.Run(ctx) }()
	go func() { results <- monitor.Run(ctx) }()
	go func() { results <- outbox.Run(ctx) }()
	go func() { results <- matcher.Run(ctx) }()
	go func() { results <- notifier.Run(ctx) }()
	err = <-results
	cancel()

	return err
}

func buildSources(client *http.Client) []provider.Source {
	result := make([]provider.Source, 0, 8)
	for _, property := range []domain.PropertyType{domain.PropertyApartment, domain.PropertyHouse} {
		for _, deal := range []domain.DealType{domain.DealRent, domain.DealSale} {
			result = append(result, sslv.New(property, deal, client), city24.New(property, deal, client))
		}
	}
	return result
}

type contactTransport struct {
	base  http.RoundTripper
	agent string
}

func (t contactTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	if clone.Header.Get("User-Agent") == "" {
		clone.Header.Set("User-Agent", t.agent)
	}
	return t.base.RoundTrip(clone)
}
