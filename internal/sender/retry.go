package sender

import (
	"math/rand"
	"time"
)

type ExponentialBackoff struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
	Jitter       float64
}

func NewExponentialBackoff(initial, max time.Duration) *ExponentialBackoff {
	return &ExponentialBackoff{
		InitialDelay: initial,
		MaxDelay:     max,
		Multiplier:   2.0,
		Jitter:       0.1,
	}
}

func (b *ExponentialBackoff) NextDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return b.InitialDelay
	}

	delay := float64(b.InitialDelay)
	for i := 0; i < attempt; i++ {
		delay *= b.Multiplier
		if delay > float64(b.MaxDelay) {
			delay = float64(b.MaxDelay)
			break
		}
	}

	jitter := delay * b.Jitter * (2*rand.Float64() - 1)
	delay += jitter

	if delay > float64(b.MaxDelay) {
		delay = float64(b.MaxDelay)
	}

	return time.Duration(delay)
}

func (b *ExponentialBackoff) Reset() time.Duration {
	return b.InitialDelay
}
