package config

import (
	"errors"
	"os"
	"strings"
)

const (
	defaultAppEnv   = "development"
	defaultHTTPAddr = ":8080"
)

var ErrDatabaseURLRequired = errors.New("DATABASE_URL is required")

type Config struct {
	AppEnv      string
	HTTPAddr    string
	DatabaseURL string
}

func Load() (Config, error) {
	appEnv := valueOrDefault(
		os.Getenv("APP_ENV"),
		defaultAppEnv,
	)

	httpAddr := valueOrDefault(
		os.Getenv("HTTP_ADDR"),
		defaultHTTPAddr,
	)

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return Config{}, ErrDatabaseURLRequired
	}

	return Config{
		AppEnv:      appEnv,
		HTTPAddr:    httpAddr,
		DatabaseURL: databaseURL,
	}, nil
}

func valueOrDefault(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}

	return value
}
