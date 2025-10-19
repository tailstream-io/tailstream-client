// Package main - config.go
//
// Configuration management for the Tailstream client.
//
// This file handles loading and saving client configuration to ~/.tailstream-client.yaml,
// including OAuth credentials, base URL, and default stream preferences.
// It provides functions to determine the effective base URL from flags, config, or defaults.

package main

import (
	"os"
	"os/user"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	configFileName = ".tailstream-client.yaml"
)

// ClientConfig stores the user's authentication and preferences
type ClientConfig struct {
	BaseURL       string `yaml:"base_url"`
	AccessToken   string `yaml:"access_token"`
	RefreshToken  string `yaml:"refresh_token"`
	DefaultStream string `yaml:"default_stream"`
	UpdatedAt     string `yaml:"updated_at"`
}

// getConfigPath returns the path to the config file
func getConfigPath() (string, error) {
	usr, err := user.Current()
	if err != nil {
		return "", err
	}
	return filepath.Join(usr.HomeDir, configFileName), nil
}

// loadConfig loads the client configuration from disk
func loadConfig() (*ClientConfig, error) {
	path, err := getConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config ClientConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// saveConfig saves the client configuration to disk
func saveConfig(config *ClientConfig) error {
	path, err := getConfigPath()
	if err != nil {
		return err
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// determineBaseURL returns the base URL to use
func determineBaseURL(flagValue string, config *ClientConfig) string {
	if flagValue != "" {
		return flagValue
	}
	if config != nil && config.BaseURL != "" {
		return config.BaseURL
	}
	return defaultBaseURL
}
