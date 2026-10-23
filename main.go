package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // distroless images have no zoneinfo

	"github.com/joho/godotenv"

	"remna-control/internal/api"
	"remna-control/internal/api/handlers"
	"remna-control/internal/audit"
	"remna-control/internal/backup"
	"remna-control/internal/billing"
	"remna-control/internal/config"
	"remna-control/internal/expenses"
	"remna-control/internal/fx"
	"remna-control/internal/jobs"
	"remna-control/internal/remnawave"
	"remna-control/internal/rwsync"
	"remna-control/internal/settings"
	"remna-control/internal/store"
)

// Set via -ldflags; "dev"/"none" when built with a plain `go build`.
var (
	Version = "dev"
	Commit  = "none"
)

const usage = `vpn-control — VPN business admin on top of Remnawave.

Usage:
  vpn-control [flags]
`

func main() {
	port := flag.String("port", "", "HTTP port override (defaults to $PORT or 8080)")
	noWeb := flag.Bool("no-web", false, "Don't serve the embedded SPA (use with the Bun dev server)")
	noSync := flag.Bool("no-sync", false, "Disable background Remnawave/traffic sync")
	debug := flag.Bool("debug", false, "Enable debug logging")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage); flag.PrintDefaults() }
	flag.Parse()

	if *showVersion {
		fmt.Printf("vpn-control %s (commit: %s)\n", Version, Commit)
		return
	}

	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	if *port != "" {
		cfg.Port = *port
	}
	if *debug {
		cfg.Debug = true
	}
	setupLogging(cfg.Debug)
	time.Local = cfg.Location

	slog.Info("starting", "version", Version, "commit", Commit)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	db, sqlDB, err := store.OpenDB(ctx, cfg.DBPath)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	rw := remnawave.NewClient(cfg.RemnawaveURL, cfg.RemnawaveToken, cfg.RemnawaveAPIKey)
	st := settings.New(db)
	fxs := fx.New(db, st.Base)
	syncSvc := rwsync.New(db, rw, cfg.AddonsConfig)
	expSvc := expenses.New(db, rw, fxs, cfg.Location)
	billingSvc := billing.New(db, rw, st)

	snapshots := backup.NewSnapshots(cfg.DBPath, sqlDB, Version)
	snapshotEvery := func() time.Duration {
		return time.Duration(st.Float(ctx, settings.SnapshotIntervalHours) * float64(time.Hour))
	}
	snapshotKeep := func() int { return st.Int(ctx, settings.SnapshotKeep) }

	// With -no-sync no job runs, backups included.
	mgr := jobs.New(db, !*noSync)
	mgr.Register(jobs.Job{Key: "panel_users", Every: cfg.SyncInterval, Tick: syncSvc.SyncJob, Throttle: true})
	mgr.Register(jobs.Job{Key: "traffic", Every: cfg.TrafficSyncInterval, Tick: expSvc.TrafficJob, Throttle: true})
	mgr.Register(jobs.Job{
		Key:   "panel_sync",
		Every: cfg.PanelSyncInterval,
		Tick: func(ctx context.Context) (jobs.Result, error) {
			return billingSvc.PanelSyncJob(audit.WithActor(ctx, "system"))
		},
		Manual: billingSvc.PanelSyncJob, // audited as whoever ran it
	})
	mgr.Register(jobs.Job{
		Key:      "backups",
		Every:    10 * time.Minute,
		Schedule: snapshotEvery,
		Tick:     snapshots.TickJob(snapshotEvery, snapshotKeep),
		Manual:   snapshots.ManualJob(snapshotKeep),
	})
	mgr.Start(ctx)
	if *noSync {
		if err := syncSvc.SyncAddons(ctx); err != nil {
			slog.Warn("addons sync failed", "error", err)
		}
	}

	app := api.New(&api.Deps{
		Handlers: &handlers.Handlers{
			DB: db, SQL: sqlDB, RW: rw, Billing: billingSvc, Expenses: expSvc,
			FX: fxs, Sync: syncSvc, Settings: st, Snapshots: snapshots, Jobs: mgr, Config: cfg, Version: Version,
		},
		NoWeb: *noWeb,
	})

	go func() {
		slog.Info("server listening", "port", cfg.Port)
		if err := app.Listen("0.0.0.0:" + cfg.Port); err != nil {
			slog.Error("server error", "error", err)
			cancel()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = app.ShutdownWithContext(shutdownCtx)
}

func setupLogging(debug bool) {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
}
