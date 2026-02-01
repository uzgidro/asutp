package config

import (
	"os"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	Env     string       `yaml:"env" env-default:"prod"`
	Station StationRef   `yaml:"station"`
	Sender  SenderConfig `yaml:"sender"`
	Buffer  BufferConfig `yaml:"buffer"`
	Health  HealthConfig `yaml:"health"`
	Log     LogConfig    `yaml:"log"`
}

type StationRef struct {
	ID         string `yaml:"id" env-required:"true"`
	Name       string `yaml:"name" env-required:"true"`
	ConfigPath string `yaml:"config_path" env-required:"true"`
}

type SenderConfig struct {
	URL     string        `yaml:"url" env-required:"true"`
	Token   string        `yaml:"token" env:"SENDER_TOKEN" env-required:"true"`
	Timeout time.Duration `yaml:"timeout" env-default:"30s"`
	Retry   RetryConfig   `yaml:"retry"`
}

type RetryConfig struct {
	MaxAttempts  int           `yaml:"max_attempts" env-default:"5"`
	InitialDelay time.Duration `yaml:"initial_delay" env-default:"1s"`
	MaxDelay     time.Duration `yaml:"max_delay" env-default:"60s"`
}

type BufferConfig struct {
	Enabled bool          `yaml:"enabled" env-default:"true"`
	Path    string        `yaml:"path" env-default:"/var/lib/asutp/buffer.db"`
	MaxAge  time.Duration `yaml:"max_age" env-default:"24h"`
}

type HealthConfig struct {
	Address string `yaml:"address" env-default:":8080"`
}

type LogConfig struct {
	Level  string `yaml:"level" env-default:"info"`
	Format string `yaml:"format" env-default:"json"`
}

func MustLoad(configPath string) *Config {
	if configPath == "" {
		configPath = os.Getenv("CONFIG_PATH")
	}

	if configPath == "" {
		configPath = "config/config.yaml"
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		panic("config file not found: " + configPath)
	}

	var cfg Config
	if err := cleanenv.ReadConfig(configPath, &cfg); err != nil {
		panic("failed to read config: " + err.Error())
	}

	return &cfg
}
