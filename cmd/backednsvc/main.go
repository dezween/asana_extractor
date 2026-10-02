// Command backednsvc is the composition root for the Asana extractor: it
// loads configuration, wires adapters into the extraction service, and runs
// the periodic extraction loop until interrupted.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	envloader "asana_extractor/internal/config/adapter/env"
	"asana_extractor/internal/extractor/adapter/asana"
	"asana_extractor/internal/extractor/adapter/jsonwriter"
	"asana_extractor/internal/extractor/service"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	var intervalFlag time.Duration
	flag.DurationVar(&intervalFlag, "interval", 0, "extraction interval, e.g. 5m or 30s (overrides EXTRACT_INTERVAL)")
	flag.Parse()

	cfg, err := envloader.NewLoader().Load()
	if err != nil {
		slog.Error("config error", "error", err.Error())
		os.Exit(1)
	}

	if intervalFlag > 0 {
		cfg.Interval = intervalFlag
	}

	client := asana.NewClient(cfg.AsanaToken, asana.DefaultRatePerMinute)
	defer client.Close()
	writer := jsonwriter.NewWriter(cfg.OutputDir)
	extractor := service.NewExtractor(client, writer)

	// shutdownCtx only governs when RunPeriodic stops scheduling new cycles.
	// It is deliberately NOT passed into the cycle itself, so a SIGINT/SIGTERM
	// received mid-cycle doesn't abort an in-flight Asana request — the
	// current cycle is allowed to finish (bounded by cfg.CycleTimeout), and
	// no further cycle is started afterwards.
	shutdownCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-shutdownCtx.Done()
		slog.Info("shutdown signal received: letting the current cycle finish, then exiting")
	}()

	slog.Info("starting asana extractor",
		"workspace", cfg.WorkspaceGID,
		"output_dir", cfg.OutputDir,
		"interval", cfg.Interval.String(),
		"cycle_timeout", cfg.CycleTimeout.String(),
	)

	// cfg.CycleTimeout bounds a single extraction cycle (ListUsers + all
	// writes + ListProjects + all writes) so that, even once shutdown is
	// requested, the process cannot hang forever waiting for the in-flight
	// cycle to finish before exiting. This assumes the workspace's full
	// user+project pagination completes within the configured budget
	// (default 2m, see CYCLE_TIMEOUT) — see domain.Config.CycleTimeout for
	// what happens if that assumption doesn't hold.
	service.RunPeriodic(shutdownCtx, cfg.Interval, func(context.Context) error {
		cycleCtx, cancel := context.WithTimeout(context.Background(), cfg.CycleTimeout)
		defer cancel()
		return extractor.Run(cycleCtx, cfg.WorkspaceGID)
	}, func(err error) {
		if err != nil {
			slog.Error("extraction cycle failed", "error", err.Error())
		} else {
			slog.Info("extraction cycle completed successfully")
		}
	})

	slog.Info("shutdown complete")
}
