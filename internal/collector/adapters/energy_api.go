package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/speedwagon-io/asutp/internal/collector"
	"github.com/speedwagon-io/asutp/internal/config"
	"github.com/speedwagon-io/asutp/internal/lib/logger/sl"
	"github.com/speedwagon-io/asutp/internal/model"
)

type EnergyAPIAdapter struct {
	log     *slog.Logger
	baseURL string
	client  *http.Client
}

func NewEnergyAPIAdapter(log *slog.Logger, baseURL string, timeout time.Duration) *EnergyAPIAdapter {
	return &EnergyAPIAdapter{
		log:     log,
		baseURL: baseURL,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (a *EnergyAPIAdapter) Name() string {
	return "energy_api"
}

func (a *EnergyAPIAdapter) Close() error {
	a.client.CloseIdleConnections()
	return nil
}

func (a *EnergyAPIAdapter) Collect(ctx context.Context, device *config.DeviceConfig) (*collector.CollectedData, error) {
	url := fmt.Sprintf("%s/%s", a.baseURL, device.Endpoint)

	// Build request body: {"parameter": "telemex"} or {"parameter": "telemetry"}
	requestBody := map[string]string{
		"parameter": device.RequestParam,
	}
	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Some endpoints return plain "True"/"False" instead of JSON
	// when there's no data or everything is OK
	bodyStr := string(bytes.TrimSpace(body))
	if bodyStr == "True" || bodyStr == "False" || bodyStr == "true" || bodyStr == "false" {
		a.log.Debug("endpoint returned boolean, no data to collect",
			slog.String("endpoint", device.Endpoint),
			slog.String("response", bodyStr),
		)
		return &collector.CollectedData{
			DeviceID:    device.ID,
			DeviceName:  device.Name,
			DeviceGroup: device.Group,
			DataPoints:  []model.DataPoint{},
		}, nil
	}

	// Fix Python-style booleans (True/False -> true/false)
	bodyStr = strings.ReplaceAll(bodyStr, ":True,", ":true,")
	bodyStr = strings.ReplaceAll(bodyStr, ":True}", ":true}")
	bodyStr = strings.ReplaceAll(bodyStr, ":False,", ":false,")
	bodyStr = strings.ReplaceAll(bodyStr, ":False}", ":false}")

	var rawData map[string]any
	if err := json.Unmarshal([]byte(bodyStr), &rawData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	dataPoints := a.transformData(rawData, device.Fields)

	return &collector.CollectedData{
		DeviceID:    device.ID,
		DeviceName:  device.Name,
		DeviceGroup: device.Group,
		DataPoints:  dataPoints,
	}, nil
}

func (a *EnergyAPIAdapter) transformData(rawData map[string]any, fields []config.FieldConfig) []model.DataPoint {
	dataPoints := make([]model.DataPoint, 0, len(fields))

	for _, field := range fields {
		rawValue, exists := rawData[field.Source]
		if !exists {
			a.log.Debug("field not found in response",
				slog.String("source", field.Source),
			)
			dataPoints = append(dataPoints, model.DataPoint{
				Name:    field.Target,
				Value:   nil,
				Unit:    field.Unit,
				Quality: model.QualityBad,
			})
			continue
		}

		value, quality := a.convertValue(rawValue, field.Type)

		dp := model.DataPoint{
			Name:    field.Target,
			Value:   value,
			Unit:    field.Unit,
			Quality: quality,
		}

		if field.Severity != "" {
			dp.Severity = field.Severity
		}

		dataPoints = append(dataPoints, dp)
	}

	return dataPoints
}

func (a *EnergyAPIAdapter) convertValue(rawValue any, fieldType string) (any, string) {
	if rawValue == nil {
		return nil, model.QualityBad
	}

	switch fieldType {
	case "float":
		return a.toFloat(rawValue)
	case "int":
		return a.toInt(rawValue)
	case "bool":
		return a.toBool(rawValue)
	case "string":
		return fmt.Sprintf("%v", rawValue), model.QualityGood
	default:
		return rawValue, model.QualityGood
	}
}

func (a *EnergyAPIAdapter) toFloat(v any) (any, string) {
	switch val := v.(type) {
	case float64:
		return val, model.QualityGood
	case float32:
		return float64(val), model.QualityGood
	case int:
		return float64(val), model.QualityGood
	case int64:
		return float64(val), model.QualityGood
	case string:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			a.log.Debug("failed to parse float", slog.String("value", val), sl.Err(err))
			return nil, model.QualityBad
		}
		return f, model.QualityGood
	default:
		return nil, model.QualityBad
	}
}

func (a *EnergyAPIAdapter) toInt(v any) (any, string) {
	switch val := v.(type) {
	case int:
		return val, model.QualityGood
	case int64:
		return int(val), model.QualityGood
	case float64:
		return int(val), model.QualityGood
	case string:
		i, err := strconv.Atoi(val)
		if err != nil {
			a.log.Debug("failed to parse int", slog.String("value", val), sl.Err(err))
			return nil, model.QualityBad
		}
		return i, model.QualityGood
	default:
		return nil, model.QualityBad
	}
}

func (a *EnergyAPIAdapter) toBool(v any) (any, string) {
	switch val := v.(type) {
	case bool:
		return val, model.QualityGood
	case int:
		return val != 0, model.QualityGood
	case float64:
		return val != 0, model.QualityGood
	case string:
		b, err := strconv.ParseBool(val)
		if err != nil {
			return val == "1" || val == "on" || val == "true", model.QualityGood
		}
		return b, model.QualityGood
	default:
		return nil, model.QualityBad
	}
}
