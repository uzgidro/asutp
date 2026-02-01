package collector

import (
	"context"

	"github.com/speedwagon-io/asutp/internal/config"
	"github.com/speedwagon-io/asutp/internal/model"
)

type CollectedData struct {
	DeviceID    string
	DeviceName  string
	DeviceGroup string
	DataPoints  []model.DataPoint
}

type Collector interface {
	Collect(ctx context.Context, device *config.DeviceConfig) (*CollectedData, error)
	Name() string
	Close() error
}
