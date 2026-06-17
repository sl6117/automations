// Package config loads non-secret JSON config files into caller-provided structs
// It is intentionally generic: it knows nothing about any project's schema
package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Load reads the JSON file at path and unmarshals it into destination (a pointer).
func Load(path string, destination any) error {
	data, err := os.ReadFile(path)

	if err != nil {
		return fmt.Errorf("read config: %s: %w", path, err)
	}
	if err := json.Unmarshal(data, destination); err != nil {
		return fmt.Errorf("parse config %s: %w", path, err)
	}
	return nil
}
