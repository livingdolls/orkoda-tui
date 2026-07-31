package artifact

import (
	"context"
	"io"
)

// Store persists execution artifacts independently from the workflow database.
type Store interface {
	Save(ctx context.Context, key string, source io.Reader) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}
