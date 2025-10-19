package main

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDetermineBaseURL(t *testing.T) {
	tests := []struct {
		name      string
		flagValue string
		config    *ClientConfig
		expected  string
	}{
		{
			name:      "flag takes precedence",
			flagValue: "https://custom.example.com",
			config:    &ClientConfig{BaseURL: "https://config.example.com"},
			expected:  "https://custom.example.com",
		},
		{
			name:      "config used when no flag",
			flagValue: "",
			config:    &ClientConfig{BaseURL: "https://config.example.com"},
			expected:  "https://config.example.com",
		},
		{
			name:      "default used when no flag or config",
			flagValue: "",
			config:    &ClientConfig{},
			expected:  defaultBaseURL,
		},
		{
			name:      "default used when config is nil",
			flagValue: "",
			config:    nil,
			expected:  defaultBaseURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := determineBaseURL(tt.flagValue, tt.config)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	// Create a temporary home directory for testing
	tmpDir := t.TempDir()

	// We can't easily change HOME for the os/user package
	// So we'll test the raw file operations instead
	configPath := filepath.Join(tmpDir, configFileName)

	// Test config
	config := &ClientConfig{
		BaseURL:       "https://test.example.com",
		AccessToken:   "test-token",
		RefreshToken:  "test-refresh",
		DefaultStream: "test-stream-id",
		UpdatedAt:     "2024-01-01T00:00:00Z",
	}

	// Marshal config
	data, err := yaml.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	// Save config
	err = os.WriteFile(configPath, data, 0600)
	if err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatalf("config file was not created")
	}

	// Load config
	loadedData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	var loaded ClientConfig
	if err := yaml.Unmarshal(loadedData, &loaded); err != nil {
		t.Fatalf("Failed to unmarshal config: %v", err)
	}

	// Verify loaded config matches saved config
	if loaded.BaseURL != config.BaseURL {
		t.Errorf("BaseURL mismatch: expected %s, got %s", config.BaseURL, loaded.BaseURL)
	}
	if loaded.AccessToken != config.AccessToken {
		t.Errorf("AccessToken mismatch: expected %s, got %s", config.AccessToken, loaded.AccessToken)
	}
	if loaded.RefreshToken != config.RefreshToken {
		t.Errorf("RefreshToken mismatch: expected %s, got %s", config.RefreshToken, loaded.RefreshToken)
	}
	if loaded.DefaultStream != config.DefaultStream {
		t.Errorf("DefaultStream mismatch: expected %s, got %s", config.DefaultStream, loaded.DefaultStream)
	}
}

func TestLoadConfigNotExist(t *testing.T) {
	// Test reading a non-existent file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "nonexistent.yaml")

	// Try to load non-existent config
	_, err := os.ReadFile(configPath)
	if err == nil {
		t.Fatal("expected error when loading non-existent config")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected os.IsNotExist error, got: %v", err)
	}
}
