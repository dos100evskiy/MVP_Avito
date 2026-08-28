// Package config — конфигурация restaurant-service: адрес и ключ Venue API
// core-сервиса, к которому это заведение подключается как внешний клиент.
package config

import "os"

type Cfg struct {
	HTTPPort    string
	CoreAPIURL  string
	CoreAPIKey  string
	PollEnabled bool
}

func Load() Cfg {
	return Cfg{
		HTTPPort:    getEnv("HTTP_PORT", "8090"),
		CoreAPIURL:  getEnv("CORE_API_URL", "http://core:8080/api/v1/venue"),
		CoreAPIKey:  getEnv("CORE_API_KEY", "dev-venue-key"),
		PollEnabled: getEnv("POLL_ENABLED", "true") == "true",
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
