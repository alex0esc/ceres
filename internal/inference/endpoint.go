package inference

import (
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
