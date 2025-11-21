/*
Copyright 2024 The Kairos CAPI Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing
permissions and limitations under the License.
*/

package config

import (
	"os"
	"strings"
)

// Config holds configuration for the controller manager
type Config struct {
	// WatchNamespace restricts the controller to a single namespace
	// If empty, watches all namespaces
	WatchNamespace string

	// LogLevel sets the logging verbosity (debug, info, warn, error)
	LogLevel string
}

// LoadConfig loads configuration from environment variables
func LoadConfig() *Config {
	cfg := &Config{
		WatchNamespace: os.Getenv("WATCH_NAMESPACE"),
		LogLevel:        getEnvOrDefault("LOG_LEVEL", "info"),
	}
	return cfg
}

// getEnvOrDefault returns the environment variable value or a default
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// ShouldWatchAllNamespaces returns true if the controller should watch all namespaces
func (c *Config) ShouldWatchAllNamespaces() bool {
	return c.WatchNamespace == ""
}

// GetWatchNamespace returns the namespace to watch, or empty string for all namespaces
func (c *Config) GetWatchNamespace() string {
	return strings.TrimSpace(c.WatchNamespace)
}

