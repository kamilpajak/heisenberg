package llm

import (
	"context"
	"fmt"
	"os"
)

// Provider represents an LLM provider
type Provider string

const (
	ProviderGoogle    Provider = "google"
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
)

// Message represents a chat message
type Message struct {
	Role    string `json:"role"` // "user", "assistant", "system"
	Content string `json:"content"`
}

// Response represents an LLM response
type Response struct {
	Content      string
	InputTokens  int
	OutputTokens int
	Model        string
}

// Client is the interface for LLM providers
type Client interface {
	// Complete sends a prompt and returns the completion
	Complete(ctx context.Context, messages []Message) (*Response, error)

	// Provider returns the provider name
	Provider() Provider

	// Model returns the model name
	Model() string
}

// Config holds configuration for creating an LLM client
type Config struct {
	Provider Provider
	Model    string
	APIKey   string
}

// NewClient creates a new LLM client based on the provider
func NewClient(cfg Config) (Client, error) {
	// Get API key from environment if not provided
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = getAPIKeyFromEnv(cfg.Provider)
	}

	if apiKey == "" {
		return nil, fmt.Errorf("API key required for provider %s", cfg.Provider)
	}

	switch cfg.Provider {
	case ProviderGoogle:
		model := cfg.Model
		if model == "" {
			model = "gemini-2.0-flash"
		}
		return NewGoogleClient(apiKey, model), nil

	case ProviderOpenAI:
		model := cfg.Model
		if model == "" {
			model = "gpt-4o-mini"
		}
		return NewOpenAIClient(apiKey, model), nil

	case ProviderAnthropic:
		model := cfg.Model
		if model == "" {
			model = "claude-sonnet-4-20250514"
		}
		return NewAnthropicClient(apiKey, model), nil

	default:
		return nil, fmt.Errorf("unknown provider: %s", cfg.Provider)
	}
}

func getAPIKeyFromEnv(provider Provider) string {
	switch provider {
	case ProviderGoogle:
		return os.Getenv("GOOGLE_API_KEY")
	case ProviderOpenAI:
		return os.Getenv("OPENAI_API_KEY")
	case ProviderAnthropic:
		return os.Getenv("ANTHROPIC_API_KEY")
	default:
		return ""
	}
}
