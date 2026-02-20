package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const configFileName = ".pxve.yaml"

// InstanceConfig holds configuration for a single Proxmox instance.
type InstanceConfig struct {
	URL         string `yaml:"url"`
	TokenID     string `yaml:"token-id,omitempty"`
	TokenSecret string `yaml:"token-secret,omitempty"`
	Username    string `yaml:"username,omitempty"`
	Password    string `yaml:"password,omitempty"`
	VerifyTLS   bool   `yaml:"verify-tls,omitempty"`
}

// Config is the top-level configuration structure.
type Config struct {
	CurrentInstance string                    `yaml:"current-instance,omitempty"`
	Instances       map[string]InstanceConfig `yaml:"instances,omitempty"`
}

// configPath returns the path to the config file.
func configPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return configFileName
	}
	return filepath.Join(home, configFileName)
}

// Load reads the config file and returns a Config.
func Load() (*Config, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{Instances: map[string]InstanceConfig{}}, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if cfg.Instances == nil {
		cfg.Instances = map[string]InstanceConfig{}
	}
	return &cfg, nil
}

// Save writes the config to disk.
func Save(cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}
	if err := os.WriteFile(configPath(), data, 0600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

// Resolve returns the InstanceConfig to use based on priority:
// 1. named instance (from --instance flag or PROXMOX_INSTANCE env)
// 2. current-instance in config
// 3. inline parameters (passed separately by the caller)
func (c *Config) Resolve(instanceName string) (*InstanceConfig, string, error) {
	if instanceName == "" {
		instanceName = os.Getenv("PROXMOX_INSTANCE")
	}
	if instanceName == "" {
		instanceName = c.CurrentInstance
	}
	if instanceName == "" {
		return nil, "", fmt.Errorf("no instance selected — run 'pxve instance use <name>' or set PROXMOX_INSTANCE")
	}

	inst, ok := c.Instances[instanceName]
	if !ok {
		return nil, "", fmt.Errorf("instance %q not found in config", instanceName)
	}
	return &inst, instanceName, nil
}
