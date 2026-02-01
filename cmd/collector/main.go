package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/speedwagon-io/asutp/internal/buffer"
	"github.com/speedwagon-io/asutp/internal/collector"
	"github.com/speedwagon-io/asutp/internal/collector/adapters"
	"github.com/speedwagon-io/asutp/internal/config"
	"github.com/speedwagon-io/asutp/internal/health"
	"github.com/speedwagon-io/asutp/internal/lib/logger/sl"
	"github.com/speedwagon-io/asutp/internal/sender"
)

func main() {
	configPath := flag.String("config", "", "path to config file")
	dryRun := flag.Bool("dry-run", false, "log data instead of sending")
	flag.Parse()

	cfg := config.MustLoad(*configPath)

	log := sl.SetupLogger(cfg.Log.Level, cfg.Log.Format)

	log.Info("starting ASUTP collector",
		slog.String("env", cfg.Env),
		slog.String("station_id", cfg.Station.ID),
		slog.Bool("dry_run", *dryRun),
	)

	stationCfg := config.MustLoadStation(cfg.Station.ConfigPath)

	log.Info("loaded station config",
		slog.String("station_id", stationCfg.StationID),
		slog.String("station_name", stationCfg.StationName),
		slog.Int("devices", len(stationCfg.Devices)),
	)

	var coll collector.Collector
	switch stationCfg.Connection.Adapter {
	case "energy_api":
		coll = adapters.NewEnergyAPIAdapter(
			log,
			stationCfg.Connection.BaseURL,
			stationCfg.Connection.Timeout,
		)
	default:
		log.Error("unknown adapter", slog.String("adapter", stationCfg.Connection.Adapter))
		os.Exit(1)
	}

	// Use LogSender for dry-run mode, HTTPSender otherwise
	var dataSender sender.Sender
	if *dryRun {
		dataSender = sender.NewLogSender(log)
		log.Info("dry-run mode: data will be logged instead of sent")
	} else {
		dataSender = sender.NewHTTPSender(log, &cfg.Sender)
	}

	var buf buffer.Buffer
	if cfg.Buffer.Enabled && !*dryRun {
		var err error
		buf, err = buffer.NewSQLiteBuffer(log, cfg.Buffer.Path)
		if err != nil {
			log.Error("failed to create buffer", sl.Err(err))
			os.Exit(1)
		}
		log.Info("buffer enabled", slog.String("path", cfg.Buffer.Path))
	}

	healthServer := health.NewServer(log, cfg.Health.Address)

	healthServer.AddChecker(health.NewSenderHealthChecker(dataSender.Health))

	if buf != nil {
		if sqliteBuf, ok := buf.(*buffer.SQLiteBuffer); ok {
			healthServer.AddChecker(health.NewBufferHealthChecker(sqliteBuf.Count))
		}
	}

	if err := healthServer.Start(); err != nil {
		log.Error("failed to start health server", sl.Err(err))
		os.Exit(1)
	}

	manager := collector.NewManager(log, cfg, stationCfg, coll, dataSender, buf)

	ctx, cancel := context.WithCancel(context.Background())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Info("received signal, shutting down", slog.String("signal", sig.String()))
		cancel()
	}()

	manager.Start(ctx)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10)
	defer shutdownCancel()

	manager.Stop()

	if err := healthServer.Stop(shutdownCtx); err != nil {
		log.Error("failed to stop health server", sl.Err(err))
	}

	if buf != nil {
		if err := buf.Close(); err != nil {
			log.Error("failed to close buffer", sl.Err(err))
		}
	}

	log.Info("collector stopped")
}
