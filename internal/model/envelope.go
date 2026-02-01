package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Envelope struct {
	ID          string      `json:"id"`
	StationID   string      `json:"station_id"`
	StationName string      `json:"station_name"`
	Timestamp   time.Time   `json:"timestamp"`
	DeviceID    string      `json:"device_id"`
	DeviceName  string      `json:"device_name"`
	DeviceGroup string      `json:"device_group"`
	Values      []DataPoint `json:"values"`
}

func NewEnvelope(stationID, stationName, deviceID, deviceName, deviceGroup string, values []DataPoint) *Envelope {
	return &Envelope{
		ID:          uuid.New().String(),
		StationID:   stationID,
		StationName: stationName,
		Timestamp:   time.Now().UTC(),
		DeviceID:    deviceID,
		DeviceName:  deviceName,
		DeviceGroup: deviceGroup,
		Values:      values,
	}
}

func (e *Envelope) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

func EnvelopeFromJSON(data []byte) (*Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, err
	}
	return &e, nil
}
