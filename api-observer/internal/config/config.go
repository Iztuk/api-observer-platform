// Package config handles the configuration loading and validation
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	AppLog      string `yaml:"app_log"`
	FindingsLog string `yaml:"findings_log"` // WAL will share the same directory
	RuleSetPath string `yaml:"rules"`

	IngestAddress string `yaml:"ingest_address"`
	DashboardPort string `yaml:"dashboard_port"`
	QueueSize     int    `yaml:"queue_size"`
	WorkerCount   int    `yaml:"worker_count"`

	Nodes []NodeConfig `yaml:"nodes"`

	FilePath string `yaml:"-"`
}

type NodeConfig struct {
	Name string `yaml:"name"`
	Addr string `yaml:"addr"`
}

func LoadConfigurationFile() (*Config, error) {
	cfgFilePath := os.Getenv("API_OBSERVER_CONFIG")

	if cfgFilePath == "" {
		cfgDir, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf(
				"could not find config directory: %w",
				err,
			)
		}

		appConfigDir := filepath.Join(
			cfgDir,
			"api-observer",
		)

		cfgFilePath = filepath.Join(
			appConfigDir,
			"config.yaml",
		)

		cfg, err := loadConfiguration(cfgFilePath)

		switch {
		case err == nil:
			return cfg, nil

		case errors.Is(err, os.ErrNotExist):
			c, err := DefaultConfig()
			if err != nil {
				return nil, fmt.Errorf("%s", err.Error())
			}
			cfg = c

			if err := cfg.Validate(); err != nil {
				return nil, fmt.Errorf(
					"invalid default configuration: %w",
					err,
				)
			}

			if err := writeDefaultConfiguration(
				appConfigDir,
				cfgFilePath,
				cfg,
			); err != nil {
				return nil, fmt.Errorf(
					"could not create default configuration: %w",
					err,
				)
			}

			return cfg, nil

		default:
			return nil, fmt.Errorf(
				"could not load config file %q: %w",
				cfgFilePath,
				err,
			)
		}
	}

	cfg, err := loadConfiguration(cfgFilePath)
	if err != nil {
		return nil, fmt.Errorf(
			"could not load config file %q: %w",
			cfgFilePath,
			err,
		)
	}
	cfg.FilePath = cfgFilePath

	return cfg, nil
}

func SaveConfigurationFile(cfg *Config) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf(
			"invalid configuration: %w",
			err,
		)
	}

	cfgDir := filepath.Dir(cfg.FilePath)

	if err := os.MkdirAll(
		cfgDir,
		0o700,
	); err != nil {
		return fmt.Errorf(
			"failed to create config directory %q: %w",
			cfgDir,
			err,
		)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf(
			"failed to encode configuration: %w",
			err,
		)
	}

	tempFile, err := os.CreateTemp(
		cfgDir,
		".config-*.yaml",
	)
	if err != nil {
		return fmt.Errorf(
			"failed to create temporary config file: %w",
			err,
		)
	}

	tempPath := tempFile.Name()

	// If anything fails before the rename, clean up
	// the temporary file.
	defer os.Remove(tempPath)

	if err := tempFile.Chmod(0o600); err != nil {
		tempFile.Close()

		return fmt.Errorf(
			"failed to set temporary config permissions: %w",
			err,
		)
	}

	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()

		return fmt.Errorf(
			"failed to write configuration: %w",
			err,
		)
	}

	if err := tempFile.Sync(); err != nil {
		tempFile.Close()

		return fmt.Errorf(
			"failed to sync configuration: %w",
			err,
		)
	}

	if err := tempFile.Close(); err != nil {
		return fmt.Errorf(
			"failed to close temporary config file: %w",
			err,
		)
	}

	if err := os.Rename(
		tempPath,
		cfg.FilePath,
	); err != nil {
		return fmt.Errorf(
			"failed to replace config file %q: %w",
			cfg.FilePath,
			err,
		)
	}

	return nil
}

func DefaultConfig() (*Config, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get user config directory: %w",
			err,
		)
	}

	appDir := filepath.Join(configDir, "api-observer")

	if err := os.MkdirAll(appDir, 0o700); err != nil {
		return nil, fmt.Errorf(
			"failed to create application config directory: %w",
			err,
		)
	}

	return &Config{
		AppLog: filepath.Join(
			appDir,
			"logs",
			"api-observer.log",
		),

		FindingsLog: filepath.Join(
			appDir,
			"logs",
			"findings.jsonl",
		),

		RuleSetPath: filepath.Join(
			appDir,
			"rules.yaml",
		),

		IngestAddress: ":24899",
		DashboardPort: ":8080",
		QueueSize:     1000,
		WorkerCount:   5,

		Nodes: make([]NodeConfig, 0),
	}, nil
}

func loadConfiguration(path string) (*Config, error) {
	var cfg *Config

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)

	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf(
			"failed to decode config file: %w",
			err,
		)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf(
			"invalid configuration: %w",
			err,
		)
	}

	return cfg, nil
}

func writeDefaultConfiguration(
	configDir string,
	configPath string,
	cfg *Config,
) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf(
			"failed to create config directory %q: %w",
			configDir,
			err,
		)
	}

	file, err := os.OpenFile(
		configPath,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o600,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to create config file %q: %w",
			configPath,
			err,
		)
	}
	defer file.Close()

	encoder := yaml.NewEncoder(file)
	defer encoder.Close()

	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf(
			"failed to encode default configuration: %w",
			err,
		)
	}

	return nil
}

func (cfg Config) Validate() error {
	if cfg.AppLog == "" {
		return fmt.Errorf("app_log cannot be empty")
	}

	if cfg.FindingsLog == "" {
		return fmt.Errorf("findings_log cannot be empty")
	}

	if cfg.IngestAddress == "" {
		return fmt.Errorf("ingest_address cannot be empty")
	}

	if cfg.DashboardPort == "" {
		return fmt.Errorf("dashboard_port cannot be empty")
	}

	if cfg.QueueSize <= 0 {
		return fmt.Errorf(
			"queue_size must be greater than 0",
		)
	}

	if cfg.WorkerCount <= 0 {
		return fmt.Errorf(
			"worker_count must be greater than 0",
		)
	}

	return nil
}

func (cfg Config) IngestAdvertiseAddress() (string, error) {
	host, port, err := net.SplitHostPort(
		cfg.IngestAddress,
	)
	if err != nil {
		return "",
			fmt.Errorf(
				"invalid ingest address %q: %w",
				cfg.IngestAddress,
				err,
			)
	}

	if host == "" {
		host = "localhost"
	}

	return net.JoinHostPort(
		host,
		port,
	), nil
}
