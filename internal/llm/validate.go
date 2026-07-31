package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

func validateRequest(provider string, request Request) error {
	if strings.TrimSpace(request.Model) == "" {
		return invalidRequest(provider, "model is required")
	}
	if len(request.Messages) == 0 {
		return invalidRequest(provider, "at least one message is required")
	}
	for index, message := range request.Messages {
		switch message.Role {
		case RoleSystem, RoleUser, RoleAssistant:
		default:
			return invalidRequest(provider, fmt.Sprintf("message %d has an invalid role", index))
		}
		if strings.TrimSpace(message.Content) == "" {
			return invalidRequest(provider, fmt.Sprintf("message %d content is required", index))
		}
	}
	if request.MaxOutputTokens < 0 {
		return invalidRequest(provider, "max output tokens cannot be negative")
	}
	if request.Temperature < 0 || request.Temperature > 2 {
		return invalidRequest(provider, "temperature must be between 0 and 2")
	}
	if len(request.ResponseSchema) > 0 && !json.Valid(request.ResponseSchema) {
		return invalidRequest(provider, "response schema must be valid JSON")
	}
	return nil
}

func invalidRequest(provider, message string) error {
	return &ProviderError{
		Provider: provider,
		Code:     ErrorInvalidRequest,
		Message:  message,
	}
}
