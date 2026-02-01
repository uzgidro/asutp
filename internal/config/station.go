package config

import (
	"os"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

type StationConfig struct {
	StationID   string           `yaml:"station_id"`
	StationName string           `yaml:"station_name"`
	Connection  ConnectionConfig `yaml:"connection"`
	Polling     PollingConfig    `yaml:"polling"`
	Devices     []DeviceConfig   `yaml:"devices"`
}

type ConnectionConfig struct {
	BaseURL string        `yaml:"base_url"`
	Adapter string        `yaml:"adapter" env-default:"energy_api"`
	Timeout time.Duration `yaml:"timeout" env-default:"10s"`
}

type PollingConfig struct {
	Interval time.Duration `yaml:"interval" env-default:"10s"`
	Timeout  time.Duration `yaml:"timeout" env-default:"5s"`
}

type DeviceConfig struct {
	ID           string        `yaml:"id"`
	Name         string        `yaml:"name"`
	Group        string        `yaml:"group"`
	Endpoint     string        `yaml:"endpoint"`
	RequestParam string        `yaml:"request_param"`
	Fields       []FieldConfig `yaml:"fields"`
}

type FieldConfig struct {
	Source   string `yaml:"source"`
	Target   string `yaml:"target"`
	Unit     string `yaml:"unit,omitempty"`
	Type     string `yaml:"type" env-default:"float"`
	Severity string `yaml:"severity,omitempty"`
}

func MustLoadStation(configPath string) *StationConfig {
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		panic("station config file not found: " + configPath)
	}

	var cfg StationConfig
	if err := cleanenv.ReadConfig(configPath, &cfg); err != nil {
		panic("failed to read station config: " + err.Error())
	}

	return &cfg
}
