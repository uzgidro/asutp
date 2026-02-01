package collector

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/speedwagon-io/asutp/internal/buffer"
	"github.com/speedwagon-io/asutp/internal/config"
	"github.com/speedwagon-io/asutp/internal/lib/logger/sl"
	"github.com/speedwagon-io/asutp/internal/model"
	"github.com/speedwagon-io/asutp/internal/sender"
)

type Manager struct {
	log           *slog.Logger
	cfg           *config.Config
	stationCfg    *config.StationConfig
	collector     Collector
	sender        sender.Sender
	buffer        buffer.Buffer
	stopCh        chan struct{}
	wg            sync.WaitGroup
	bufferEnabled bool
}

func NewManager(
	log *slog.Logger,
	cfg *config.Config,
	stationCfg *config.StationConfig,
	collector Collector,
	sender sender.Sender,
	buffer buffer.Buffer,
) *Manager {
	return &Manager{
		log:           log,
		cfg:           cfg,
		stationCfg:    stationCfg,
		collector:     collector,
		sender:        sender,
		buffer:        buffer,
		stopCh:        make(chan struct{}),
		bufferEnabled: cfg.Buffer.Enabled,
	}
}

func (m *Manager) Start(ctx context.Context) {
	m.log.Info("starting collector manager",
		slog.String("station_id", m.stationCfg.StationID),
		slog.Duration("interval", m.stationCfg.Polling.Interval),
	)

	ticker := time.NewTicker(m.stationCfg.Polling.Interval)
	defer ticker.Stop()

	m.wg.Add(1)
	go m.retryBufferedData(ctx)

	m.collectAndSend(ctx)

	for {
		select {
		case <-ctx.Done():
			m.log.Info("context cancelled, stopping manager")
			return
		case <-m.stopCh:
			m.log.Info("stop signal received, stopping manager")
			return
		case <-ticker.C:
			m.collectAndSend(ctx)
		}
	}
}

func (m *Manager) Stop() {
	close(m.stopCh)
	m.wg.Wait()
	if err := m.collector.Close(); err != nil {
		m.log.Error("failed to close collector", sl.Err(err))
	}
}

func (m *Manager) collectAndSend(ctx context.Context) {
	var wg sync.WaitGroup
	results := make(chan *CollectedData, len(m.stationCfg.Devices))

	for i := range m.stationCfg.Devices {
		device := &m.stationCfg.Devices[i]
		wg.Add(1)
		go func(d *config.DeviceConfig) {
			defer wg.Done()

			collectCtx, cancel := context.WithTimeout(ctx, m.stationCfg.Polling.Timeout)
			defer cancel()

			data, err := m.collector.Collect(collectCtx, d)
			if err != nil {
				m.log.Error("failed to collect data",
					slog.String("device_id", d.ID),
					sl.Err(err),
				)
				return
			}
			results <- data
		}(device)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for data := range results {
		// Skip empty data (e.g., when endpoint returns "True"/"False")
		if len(data.DataPoints) == 0 {
			m.log.Debug("skipping empty data",
				slog.String("device_id", data.DeviceID),
			)
			continue
		}

		envelope := model.NewEnvelope(
			m.stationCfg.StationID,
			m.stationCfg.StationName,
			data.DeviceID,
			data.DeviceName,
			data.DeviceGroup,
			data.DataPoints,
		)

		if err := m.sender.Send(ctx, envelope); err != nil {
			m.log.Error("failed to send data",
				slog.String("device_id", data.DeviceID),
				sl.Err(err),
			)

			if m.bufferEnabled && m.buffer != nil {
				if bufErr := m.buffer.Store(ctx, envelope); bufErr != nil {
					m.log.Error("failed to buffer data",
						slog.String("device_id", data.DeviceID),
						sl.Err(bufErr),
					)
				} else {
					m.log.Info("data buffered for later retry",
						slog.String("device_id", data.DeviceID),
					)
				}
			}
		} else {
			m.log.Debug("data sent successfully",
				slog.String("device_id", data.DeviceID),
			)
		}
	}
}

func (m *Manager) retryBufferedData(ctx context.Context) {
	defer m.wg.Done()

	if !m.bufferEnabled || m.buffer == nil {
		return
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.processBufferedData(ctx)
		}
	}
}

func (m *Manager) processBufferedData(ctx context.Context) {
	pending, err := m.buffer.GetPending(ctx, 100)
	if err != nil {
		m.log.Error("failed to get pending data from buffer", sl.Err(err))
		return
	}

	if len(pending) == 0 {
		return
	}

	m.log.Info("processing buffered data", slog.Int("count", len(pending)))

	var sentIDs []string
	for _, envelope := range pending {
		if err := m.sender.Send(ctx, envelope); err != nil {
			m.log.Debug("failed to send buffered data",
				slog.String("id", envelope.ID),
				sl.Err(err),
			)
			break
		}
		sentIDs = append(sentIDs, envelope.ID)
	}

	if len(sentIDs) > 0 {
		if err := m.buffer.MarkSent(ctx, sentIDs); err != nil {
			m.log.Error("failed to mark buffered data as sent", sl.Err(err))
		} else {
			m.log.Info("buffered data sent successfully", slog.Int("count", len(sentIDs)))
		}
	}

	if err := m.buffer.Cleanup(ctx, m.cfg.Buffer.MaxAge); err != nil {
		m.log.Error("failed to cleanup old buffer data", sl.Err(err))
	}
}
