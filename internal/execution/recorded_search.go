package execution

import (
	"context"

	"github.com/livingdolls/orkoda-tui/internal/agentconfig"
)

func (t *RecordedTools) Search(ctx context.Context, query string, maxResults int) ([]string, error) {
	value, err := t.invoke(ctx, agentconfig.ToolFileSearch, map[string]any{
		"query": query, "max_results": maxResults,
	}, func() (any, error) {
		return t.toolset.Search(query, maxResults)
	})
	if err != nil {
		return nil, err
	}
	return value.([]string), nil
}
