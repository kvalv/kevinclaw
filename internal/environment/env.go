package environment

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/joho/godotenv"
)

type Environment struct {
	SLACK_BOT_TOKEN string
	SLACK_APP_TOKEN string
	OWNER_USER_ID   string
	DATABASE_URL    string

	GOOGLE_CLIENT_ID     string
	GOOGLE_CLIENT_SECRET string
	GOOGLE_REFRESH_TOKEN string
}

func New() (Environment, error) {
	// Load .env from project root (where this source file lives: internal/environment/)
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	_ = godotenv.Load(filepath.Join(root, ".env"))

	var err error
	env := Environment{}

	if env.SLACK_BOT_TOKEN, err = parseStr("SLACK_BOT_TOKEN"); err != nil {
		return env, err
	}
	if env.SLACK_APP_TOKEN, err = parseStr("SLACK_APP_TOKEN"); err != nil {
		return env, err
	}
	if env.OWNER_USER_ID, err = parseStr("OWNER_USER_ID"); err != nil {
		return env, err
	}
	if env.DATABASE_URL, err = parseStr("DATABASE_URL"); err != nil {
		return env, err
	}

	// Optional: Google Calendar (no error if missing)
	env.GOOGLE_CLIENT_ID, _ = parseStr("GOOGLE_CLIENT_ID")
	env.GOOGLE_CLIENT_SECRET, _ = parseStr("GOOGLE_CLIENT_SECRET")
	env.GOOGLE_REFRESH_TOKEN, _ = parseStr("GOOGLE_REFRESH_TOKEN")

	return env, nil
}

func parseStr(key string) (string, error) {
	val, ok := os.LookupEnv(key)
	if !ok {
		return "", fmt.Errorf("environment variable %s not set", key)
	}
	return val, nil
}
