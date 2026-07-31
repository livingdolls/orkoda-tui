package llm

import (
	"sort"
	"strings"
)

type Catalog struct {
	registry        *Registry
	defaultProvider string
}

func NewCatalog(registry *Registry, defaultProvider string) *Catalog {
	return &Catalog{
		registry:        registry,
		defaultProvider: normalizeProviderName(defaultProvider),
	}
}

func (c *Catalog) List() []ProviderInfo {
	if c == nil || c.registry == nil {
		return []ProviderInfo{}
	}
	c.registry.mu.RLock()
	providers := make([]Provider, 0, len(c.registry.providers))
	for _, provider := range c.registry.providers {
		providers = append(providers, provider)
	}
	c.registry.mu.RUnlock()

	infos := make([]ProviderInfo, 0, len(providers))
	for _, provider := range providers {
		info := ProviderInfo{
			Name:       normalizeProviderName(provider.Name()),
			Configured: true,
		}
		if describer, ok := provider.(ProviderDescriber); ok {
			info = describer.Info()
			info.Name = normalizeProviderName(info.Name)
			if info.Name == "" {
				info.Name = normalizeProviderName(provider.Name())
			}
		}
		info.Default = strings.EqualFold(info.Name, c.defaultProvider)
		infos = append(infos, info)
	}
	sort.Slice(infos, func(left, right int) bool {
		if infos[left].Default != infos[right].Default {
			return infos[left].Default
		}
		return infos[left].Name < infos[right].Name
	})
	return infos
}
