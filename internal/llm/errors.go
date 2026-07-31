package llm

import (
	"errors"
	"fmt"
	"time"
)

type ErrorCode string

const (
	ErrorAuthentication ErrorCode = "AUTHENTICATION"
	ErrorRateLimited     ErrorCode = "RATE_LIMITED"
	ErrorTimeout         ErrorCode = "TIMEOUT"
	ErrorCancelled       ErrorCode = "CANCELLED"
	ErrorContextLength   ErrorCode = "CONTEXT_LENGTH"
	ErrorInvalidRequest  ErrorCode = "INVALID_REQUEST"
	ErrorInvalidResponse ErrorCode = "INVALID_RESPONSE"
	ErrorUnavailable     ErrorCode = "UNAVAILABLE"
	ErrorUnknown         ErrorCode = "UNKNOWN"
)

var (
	ErrProviderNotFound = errors.New("LLM provider not found")
	ErrProviderExists   = errors.New("LLM provider already registered")
)

type ProviderError struct {
	Provider   string
	Code       ErrorCode
	Message    string
	Retryable  bool
	RetryAfter time.Duration
	Cause      error
}

func (e *ProviderError) Error() string {
	if e == nil {
		return ""
	}
	provider := e.Provider
	if provider == "" {
		provider = "LLM"
	}
	message := e.Message
	if message == "" {
		message = string(e.Code)
	}
	return fmt.Sprintf("%s provider error (%s): %s", provider, e.Code, message)
}

func (e *ProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func AsProviderError(err error) (*ProviderError, bool) {
	var providerError *ProviderError
	if errors.As(err, &providerError) {
		return providerError, true
	}
	return nil, false
}
