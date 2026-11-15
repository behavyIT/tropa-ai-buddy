package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is read from environment variables (see .env.example).
type Config struct {
	Addr          string
	DatabaseURL   string
	DBSchema      string
	RedisURL      string
	CorpMCPURL    string // empty → embedded in-process corporate mock
	CorpToken     string
	CorpLatency   time.Duration
	SessionSecret string
	SecureCookie  bool
	DemoMode      bool
	SimStart      time.Time
	LLMProvider   string // none | anthropic
	AnthropicKey  string
	AnthropicURL  string
	LLMModel      string
	LLMEffort     string
	LLMFallback   bool
	CronInterval  time.Duration
	LogLevel      string
	DisableCron   bool
}

func env(k, def string) string {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	v, ok := os.LookupEnv(k)
	if !ok || v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

// LoadConfig reads configuration from the environment.
func LoadConfig() (Config, error) {
	c := Config{
		Addr:          env("HTTP_ADDR", ":8080"),
		DatabaseURL:   env("DATABASE_URL", "postgres://localhost:5432/tropa?sslmode=disable"),
		DBSchema:      env("DB_SCHEMA", ""),
		RedisURL:      env("REDIS_URL", ""),
		CorpMCPURL:    env("CORP_MCP_URL", ""),
		CorpToken:     env("CORP_TOKEN", "dev-corp-token"),
		SessionSecret: env("SESSION_SECRET", ""),
		SecureCookie:  envBool("SECURE_COOKIE", false),
		DemoMode:      envBool("DEMO_MODE", true),
		LLMProvider:   strings.ToLower(env("LLM_PROVIDER", "none")),
		AnthropicKey:  env("ANTHROPIC_API_KEY", ""),
		AnthropicURL:  env("ANTHROPIC_BASE_URL", ""),
		LLMModel:      env("LLM_MODEL", "claude-opus-5-5"),
		LLMEffort:     env("LLM_EFFORT", "low"),
		LLMFallback:   envBool("LLM_SERVER_FALLBACK", true),
		LogLevel:      env("LOG_LEVEL", "info"),
		DisableCron:   envBool("DISABLE_CRON", false),
	}
	var err error
	if c.SimStart, err = time.Parse(time.RFC3339, env("SIM_START", "2026-11-09T09:30:00+03:00")); err != nil {
		return c, fmt.Errorf("SIM_START: %w", err)
	}
	if c.CronInterval, err = time.ParseDuration(env("CRON_INTERVAL", "20s")); err != nil {
		return c, fmt.Errorf("CRON_INTERVAL: %w", err)
	}
	if c.CorpLatency, err = time.ParseDuration(env("CORP_LATENCY", "350ms")); err != nil {
		return c, fmt.Errorf("CORP_LATENCY: %w", err)
	}
	if c.SessionSecret == "" {
		if !c.DemoMode {
			return c, fmt.Errorf("SESSION_SECRET is required when DEMO_MODE=false")
		}
		c.SessionSecret = "demo-only-session-secret-change-me"
	}
	if c.LLMProvider == "anthropic" && c.AnthropicKey == "" {
		return c, fmt.Errorf("LLM_PROVIDER=anthropic requires ANTHROPIC_API_KEY")
	}
	return c, nil
}
