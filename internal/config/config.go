package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	HTTPAddr    string
	MySQLDSN    string
	RedisAddr   string
	RedisPass   string
	RedisDB     int
	JWTSecret   string
	JWTDuration time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:    env("HTTP_ADDR", ":8080"),
		MySQLDSN:    env("MYSQL_DSN", "tasks:tasks@tcp(mysql:3306)/tasks?parseTime=true&multiStatements=true"),
		RedisAddr:   env("REDIS_ADDR", "redis:6379"),
		RedisPass:   os.Getenv("REDIS_PASSWORD"),
		JWTSecret:   os.Getenv("JWT_SECRET"),
		JWTDuration: 24 * time.Hour,
	}
	if cfg.JWTSecret == "" {
		return Config{}, fmt.Errorf("JWT_SECRET is required")
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
