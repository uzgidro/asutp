package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/speedwagon-io/asutp/internal/lib/logger/sl"
)

type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusUnhealthy Status = "unhealthy"
	StatusDegraded  Status = "degraded"
)

type ComponentHealth struct {
	Name    string `json:"name"`
	Status  Status `json:"status"`
	Message string `json:"message,omitempty"`
}

type HealthResponse struct {
	Status     Status            `json:"status"`
	Components []ComponentHealth `json:"components"`
	Timestamp  time.Time         `json:"timestamp"`
}

type HealthChecker interface {
	Name() string
	Check(ctx context.Context) (Status, string)
}

type Server struct {
	log      *slog.Logger
	address  string
	server   *http.Server
	checkers []HealthChecker
	mu       sync.RWMutex
}

func NewServer(log *slog.Logger, address string) *Server {
	return &Server{
		log:      log,
		address:  address,
		checkers: make([]HealthChecker, 0),
	}
}

func (s *Server) AddChecker(checker HealthChecker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkers = append(s.checkers, checker)
}

func (s *Server) Start() error {
	r := chi.NewRouter()

	r.Get("/health", s.handleHealth)
	r.Get("/ready", s.handleReady)
	r.Get("/live", s.handleLive)

	s.server = &http.Server{
		Addr:         s.address,
		Handler:      r,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	s.log.Info("starting health server", slog.String("address", s.address))

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log.Error("health server error", sl.Err(err))
		}
	}()

	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	checkers := make([]HealthChecker, len(s.checkers))
	copy(checkers, s.checkers)
	s.mu.RUnlock()

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	response := HealthResponse{
		Status:     StatusHealthy,
		Components: make([]ComponentHealth, 0, len(checkers)),
		Timestamp:  time.Now().UTC(),
	}

	for _, checker := range checkers {
		status, message := checker.Check(ctx)
		response.Components = append(response.Components, ComponentHealth{
			Name:    checker.Name(),
			Status:  status,
			Message: message,
		})

		if status == StatusUnhealthy {
			response.Status = StatusUnhealthy
		} else if status == StatusDegraded && response.Status == StatusHealthy {
			response.Status = StatusDegraded
		}
	}

	statusCode := http.StatusOK
	if response.Status == StatusUnhealthy {
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

type SenderHealthChecker struct {
	healthFunc func(ctx context.Context) error
}

func NewSenderHealthChecker(healthFunc func(ctx context.Context) error) *SenderHealthChecker {
	return &SenderHealthChecker{healthFunc: healthFunc}
}

func (c *SenderHealthChecker) Name() string {
	return "sender"
}

func (c *SenderHealthChecker) Check(ctx context.Context) (Status, string) {
	if err := c.healthFunc(ctx); err != nil {
		return StatusDegraded, err.Error()
	}
	return StatusHealthy, ""
}

type BufferHealthChecker struct {
	countFunc func(ctx context.Context) (int64, error)
}

func NewBufferHealthChecker(countFunc func(ctx context.Context) (int64, error)) *BufferHealthChecker {
	return &BufferHealthChecker{countFunc: countFunc}
}

func (c *BufferHealthChecker) Name() string {
	return "buffer"
}

func (c *BufferHealthChecker) Check(ctx context.Context) (Status, string) {
	count, err := c.countFunc(ctx)
	if err != nil {
		return StatusUnhealthy, err.Error()
	}

	if count > 1000 {
		return StatusDegraded, "high buffer count"
	}

	return StatusHealthy, ""
}
