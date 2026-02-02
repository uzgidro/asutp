package sender

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/speedwagon-io/asutp/internal/config"
	"github.com/speedwagon-io/asutp/internal/lib/logger/sl"
	"github.com/speedwagon-io/asutp/internal/model"
)

type Sender interface {
	Send(ctx context.Context, envelope *model.Envelope) error
	SendBatch(ctx context.Context, envelopes []*model.Envelope) error
	Health(ctx context.Context) error
}

type HTTPSender struct {
	log         *slog.Logger
	baseURL     string
	stationDBID int
	token       string
	client      *http.Client
	retry       *RetryConfig
}

type RetryConfig struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

func NewHTTPSender(log *slog.Logger, cfg *config.SenderConfig, stationDBID int) *HTTPSender {
	return &HTTPSender{
		log:         log,
		baseURL:     cfg.URL,
		stationDBID: stationDBID,
		token:       cfg.Token,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
		retry: &RetryConfig{
			MaxAttempts:  cfg.Retry.MaxAttempts,
			InitialDelay: cfg.Retry.InitialDelay,
			MaxDelay:     cfg.Retry.MaxDelay,
		},
	}
}

func (s *HTTPSender) Send(ctx context.Context, envelope *model.Envelope) error {
	data, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("failed to marshal envelope: %w", err)
	}

	return s.sendWithRetry(ctx, data)
}

func (s *HTTPSender) SendBatch(ctx context.Context, envelopes []*model.Envelope) error {
	data, err := json.Marshal(envelopes)
	if err != nil {
		return fmt.Errorf("failed to marshal envelopes: %w", err)
	}

	return s.sendWithRetry(ctx, data)
}

func (s *HTTPSender) sendWithRetry(ctx context.Context, data []byte) error {
	var lastErr error
	delay := s.retry.InitialDelay

	for attempt := 1; attempt <= s.retry.MaxAttempts; attempt++ {
		err := s.doSend(ctx, data)
		if err == nil {
			return nil
		}

		lastErr = err
		s.log.Warn("send attempt failed",
			slog.Int("attempt", attempt),
			slog.Int("max_attempts", s.retry.MaxAttempts),
			sl.Err(err),
		)

		if attempt < s.retry.MaxAttempts {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}

			delay = s.nextDelay(delay)
		}
	}

	return fmt.Errorf("all %d attempts failed: %w", s.retry.MaxAttempts, lastErr)
}

func (s *HTTPSender) doSend(ctx context.Context, data []byte) error {
	url := fmt.Sprintf("%s/%d", s.baseURL, s.stationDBID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.token)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
}

func (s *HTTPSender) nextDelay(current time.Duration) time.Duration {
	next := current * 2
	if next > s.retry.MaxDelay {
		return s.retry.MaxDelay
	}
	return next
}

func (s *HTTPSender) Health(ctx context.Context) error {
	url := fmt.Sprintf("%s/%d", s.baseURL, s.stationDBID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create health request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.token)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return fmt.Errorf("server unhealthy: status %d", resp.StatusCode)
	}

	return nil
}

// LogSender logs envelopes instead of sending them (for testing)
type LogSender struct {
	log *slog.Logger
}

func NewLogSender(log *slog.Logger) *LogSender {
	return &LogSender{log: log}
}

func (s *LogSender) Send(ctx context.Context, envelope *model.Envelope) error {
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal envelope: %w", err)
	}

	s.log.Info("SEND",
		slog.String("device_id", envelope.DeviceID),
		slog.String("device_name", envelope.DeviceName),
		slog.String("device_group", envelope.DeviceGroup),
		slog.Int("values_count", len(envelope.Values)),
		slog.String("payload", string(data)),
	)

	return nil
}

func (s *LogSender) SendBatch(ctx context.Context, envelopes []*model.Envelope) error {
	for _, envelope := range envelopes {
		if err := s.Send(ctx, envelope); err != nil {
			return err
		}
	}
	return nil
}

func (s *LogSender) Health(ctx context.Context) error {
	return nil
}
