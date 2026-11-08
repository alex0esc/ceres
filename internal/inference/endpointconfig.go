package inference

import (
	"fmt"
	"log"

	"github.com/alex0esc/ceres/internal/constants"
	"github.com/alex0esc/ceres/pkg/config"
)

type EndpointConfig struct {
	Name    string `toml:"name"`
	BaseURL string `toml:"base_url"`
	APIKey  string `toml:"api_key"`
}

var endpointsConfig *config.Config

// GetEndpointsConfig returns the shared endpoints config, which also holds the
// infrastructure settings stored in endpoints.toml.
func GetEndpointsConfig() *config.Config {
	if endpointsConfig == nil {
		log.Fatal("inference: endpoints config not initialized")
	}
	return endpointsConfig
}

// LoadEndpointsFromConfig loads all endpoints from endpoints.toml. A missing
// endpoints key is written to the file with a default endpoint.
func LoadEndpointsFromConfig() (map[string]Endpoint, error) {
	cfg, err := config.New(constants.EndpointsConfigPath)
	if err != nil {
		return nil, err
	}
	endpointsConfig = cfg

	defaults := []EndpointConfig{
		{
			Name:    "llama",
			BaseURL: "http://localhost:11434/v1",
			APIKey:  "none",
		},
	}

	endpoints := make(map[string]Endpoint)
	for _, e := range config.ReadEntry(cfg, "endpoints", defaults) {
		if e.Name == "" || e.BaseURL == "" {
			return nil, fmt.Errorf("invalid endpoint config: %+v", e)
		}

		endpoints[e.Name] = *NewEndpoint(
			e.Name,
			e.BaseURL,
			e.APIKey,
		)
	}

	return endpoints, nil
}
