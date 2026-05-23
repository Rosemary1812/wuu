package providerfactory

import (
	"fmt"
	"strings"

	"github.com/blueberrycongee/wuu/internal/config"
)

type wireProtocol string

const (
	wireOpenAIChat        wireProtocol = "openai_chat"
	wireOpenAIResponses   wireProtocol = "openai_responses"
	wireAnthropicMessages wireProtocol = "anthropic_messages"
)

type authMode string

const (
	authAPIKey         authMode = "api_key"
	authAnthropicToken authMode = "anthropic_token"
	authCodexOAuth     authMode = "codex_oauth"
)

type providerProfile struct {
	TypeName string
	Wire     wireProtocol
	Auth     authMode
}

func resolveProviderProfile(provider config.ProviderConfig) (providerProfile, error) {
	typeName := normalizeType(provider.Type)
	switch typeName {
	case "openai", "openai-compatible", "codex":
		wire, err := resolveOpenAIWire(provider.WireAPI)
		if err != nil {
			return providerProfile{}, err
		}
		return providerProfile{
			TypeName: typeName,
			Wire:     wire,
			Auth:     authAPIKey,
		}, nil
	case "openai-codex", "codex-subscription", "chatgpt-codex":
		if wire := strings.ToLower(strings.TrimSpace(provider.WireAPI)); wire != "" && wire != "responses" {
			return providerProfile{}, fmt.Errorf("provider type %q supports only responses wire_api", provider.Type)
		}
		return providerProfile{
			TypeName: typeName,
			Wire:     wireOpenAIResponses,
			Auth:     authCodexOAuth,
		}, nil
	case "anthropic", "claude", "anthropic-official":
		return providerProfile{
			TypeName: typeName,
			Wire:     wireAnthropicMessages,
			Auth:     authAnthropicToken,
		}, nil
	default:
		return providerProfile{}, fmt.Errorf("unsupported provider type %q", provider.Type)
	}
}

func resolveOpenAIWire(value string) (wireProtocol, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "chat":
		return wireOpenAIChat, nil
	case "responses":
		return wireOpenAIResponses, nil
	default:
		return "", fmt.Errorf("unsupported OpenAI wire API %q", value)
	}
}

func openAIWireConfig(wire wireProtocol) string {
	switch wire {
	case wireOpenAIResponses:
		return "responses"
	default:
		return "chat"
	}
}
