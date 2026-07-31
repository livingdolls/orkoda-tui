package llm

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

func NewRegistry(providers ...Provider) (*Registry, error) {
	registry := &Registry{providers: map[string]Provider{}}
	for _, provider := range providers {
		if err := registry.Register(provider); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *Registry) Register(provider Provider) error {
	if r == nil {
		return fmt.Errorf("LLM registry is required")
	}
	if provider == nil {
		return fmt.Errorf("LLM provider is required")
	}
	name := normalizeProviderName(provider.Name())
	if name == "" {
		return fmt.Errorf("LLM provider name is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[name]; exists {
		return fmt.Errorf("%w: %s", ErrProviderExists, name)
	}
	r.providers[name] = provider
	return nil
}

func (r *Registry) Provider(name string) (Provider, error) {
	if r == nil {
		return nil, fmt.Errorf("LLM registry is required")
	}
	name = normalizeProviderName(name)
	if name == "" {
		return nil, fmt.Errorf("%w: provider name is required", ErrProviderNotFound)
	}

	r.mu.RLock()
	provider, exists := r.providers[name]
	r.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotFound, name)
	}
	return provider, nil
}

func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	r.mu.RUnlock()
	sort.Strings(names)
	return names
}

func normalizeProviderName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
