package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

// Env holds secrets loaded from .env.
type Env struct {
	SLACK_BOT_TOKEN string
	SLACK_APP_TOKEN string
	OWNER_USER_ID   string
	DATABASE_URL    string

	GOOGLE_CLIENT_ID        string
	GOOGLE_CLIENT_SECRET    string
	GOOGLE_REFRESH_TOKEN    string
	LINEAR_API_KEY          string
	HOMEASSISTANT_API_URL   string
	HOMEASSISTANT_API_TOKEN string
}

// Config holds runtime configuration loaded from kevin.yaml.
type Config struct {
	Paths         Paths              `yaml:"paths"`
	HomeAssistant HomeAssistant      `yaml:"homeassistant"`
	HistoryLimit  int                `yaml:"history_limit"` // max channel messages as context (default 10)
	Channels      map[string]Channel `yaml:"channels"`      // channel ID → config override
}

// Channel configures per-channel behavior.
type Channel struct {
	Mode string `yaml:"mode"` // "active" = process all messages (default: mention-only)
}

func (c *Config) GetHistoryLimit() int {
	if c.HistoryLimit <= 0 {
		return 10
	}
	return c.HistoryLimit
}

type Paths struct {
	Write  []string `yaml:"write"`
	Read   []string `yaml:"read"`
	Public []string `yaml:"public"`
}

type HomeAssistant struct {
	Entities []HAEntity `yaml:"entities"`
}

type HAEntity struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	Category    string `yaml:"category"`
	Description string `yaml:"description"`
}

// LoadEnv loads secrets from .env relative to the project root.
func LoadEnv() (Env, error) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	_ = godotenv.Load(filepath.Join(root, ".env"))

	var err error
	env := Env{}

	if env.SLACK_BOT_TOKEN, err = requireEnv("SLACK_BOT_TOKEN"); err != nil {
		return env, err
	}
	if env.SLACK_APP_TOKEN, err = requireEnv("SLACK_APP_TOKEN"); err != nil {
		return env, err
	}
	if env.OWNER_USER_ID, err = requireEnv("OWNER_USER_ID"); err != nil {
		return env, err
	}
	if env.DATABASE_URL, err = requireEnv("DATABASE_URL"); err != nil {
		return env, err
	}

	env.GOOGLE_CLIENT_ID, _ = requireEnv("GOOGLE_CLIENT_ID")
	env.GOOGLE_CLIENT_SECRET, _ = requireEnv("GOOGLE_CLIENT_SECRET")
	env.GOOGLE_REFRESH_TOKEN, _ = requireEnv("GOOGLE_REFRESH_TOKEN")
	env.LINEAR_API_KEY, _ = requireEnv("LINEAR_API_KEY")
	env.HOMEASSISTANT_API_URL, _ = requireEnv("HOMEASSISTANT_API_URL")
	env.HOMEASSISTANT_API_TOKEN, _ = requireEnv("HOMEASSISTANT_API_TOKEN")

	return env, nil
}

// Load reads and parses kevin.yaml.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &cfg, nil
}

func requireEnv(key string) (string, error) {
	val, ok := os.LookupEnv(key)
	if !ok {
		return "", fmt.Errorf("environment variable %s not set", key)
	}
	return val, nil
}
