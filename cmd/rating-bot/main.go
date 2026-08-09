// Command rating-bot is the Telegram helper for rating.chgk.info tournament
// organisers.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/config"
	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/rating"
	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/store"
	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/tg"
)

func main() {
	debug := flag.Bool("debug", false, "run the test bot: no scheduled jobs")
	tokenPath := flag.String("token_path", "", "file holding the bot token")
	dataDir := flag.String("data-dir", ".", "where config.json, token and the databases live")
	flag.Parse()

	if err := run(*dataDir, *tokenPath, *debug); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(dataDir, tokenPath string, debug bool) error {
	absolute, err := filepath.Abs(dataDir)
	if err != nil {
		return err
	}
	settings, err := config.Load(absolute, tokenPath, debug)
	if err != nil {
		return fmt.Errorf("reading the configuration: %w", err)
	}

	logs, err := store.OpenLogStore(settings.LogsDB())
	if err != nil {
		return fmt.Errorf("opening %s: %w", settings.LogsDB(), err)
	}
	defer logs.Close()
	setupLogging(logs)

	db, err := store.Open(settings.BotDB())
	if err != nil {
		return fmt.Errorf("opening %s: %w", settings.BotDB(), err)
	}
	defer db.Close()

	scheduler := tg.NewScheduler()
	bot, err := tg.New(tg.Options{
		Config:    settings,
		DB:        db,
		Logs:      logs,
		Rating:    rating.New(),
		Scheduler: scheduler,
		// Set to reach a local Bot API server instead of Telegram's.
		ServerURL: os.Getenv("TELEGRAM_API_URL"),
	})
	if err != nil {
		return fmt.Errorf("starting the bot: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !settings.Debug {
		bot.ScheduleJobs()
	}
	scheduler.Start(ctx)

	slog.Debug("starting bot")
	bot.Start(ctx)
	return nil
}

func setupLogging(logs *store.LogStore) {
	console := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(store.NewHandler(logs, console)))
}
