package llm

import (
	"context"
	"testing"
)

type catalogProvider struct {
	name  string
	model string
}

func (p catalogProvider) Name() string { return p.name }
func (p catalogProvider) Complete(context.Context, Request) (Response, error) {
	return Response{}, nil
}
func (p catalogProvider) Info() ProviderInfo {
	return ProviderInfo{
		Name:             p.name,
		DefaultModel:     p.model,
		Configured:       true,
		StructuredOutput: true,
	}
}

func TestCatalogListsDefaultProviderFirst(t *testing.T) {
	registry, err := NewRegistry(
		catalogProvider{name: "local-fake", model: "fake-model"},
		catalogProvider{name: "openrouter", model: "real-model"},
	)
	if err != nil {
		t.Fatal(err)
	}
	infos := NewCatalog(registry, "openrouter").List()
	if len(infos) != 2 {
		t.Fatalf("unexpected provider count %d", len(infos))
	}
	if infos[0].Name != "openrouter" || !infos[0].Default || infos[0].DefaultModel != "real-model" {
		t.Fatalf("unexpected default provider %#v", infos[0])
	}
	if infos[1].Default {
		t.Fatalf("unexpected second default %#v", infos[1])
	}
}
