package openai

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)


type Endpoint struct {
	name   string
	client openai.Client
}

func NewEndpoint(name, baseURL, apiKey string) *Endpoint {
	return &Endpoint{
		name: name,
		client: openai.NewClient(
			option.WithBaseURL(baseURL),
			option.WithAPIKey(apiKey),
		),
	}
}

// config structs
type Config struct {
	Endpoints []EndpointConfig `toml:"endpoints"`
}

type EndpointConfig struct {
	Name    string `toml:"name"`
	BaseURL string `toml:"base_url"`
	APIKey  string `toml:"api_key"`
}

// load the endpoints into a slice
func LoadEndpointsFromConfig(path string) (map[string]Endpoint, error) {
	cfg, err := EnsureAndLoadEndpointsConfig(path)
	if err != nil {
		return nil, err
	}

	endpoints := make(map[string]Endpoint)

	for _, e := range cfg.Endpoints {
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


//create config file if missing or load current config
func EnsureAndLoadEndpointsConfig(path string) (Config, error) {
	var cfg Config
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// default config
		cfg = Config{
			Endpoints: []EndpointConfig{
				{
					Name:    "ollama",
					BaseURL: "http://localhost:11434/v1",
					APIKey:  "none",
				},
			},
		}
		data, err := toml.Marshal(cfg)
		if err != nil {
			return cfg, err
		}

		// alle fehlenden Ordner im Pfad anlegen
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return cfg, fmt.Errorf("failed to create config directory: %w", err)
		}

		if err := os.WriteFile(path, data, 0644); err != nil {
			return cfg, err
		}
		return cfg, nil
	}
	_, err := toml.DecodeFile(path, &cfg)
	return cfg, err
}
