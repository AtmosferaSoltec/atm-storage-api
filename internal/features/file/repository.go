package file

import (
	"context"
	"io"
	"time"
)

type Repository interface {
	Create(context.Context, File) error
	Get(context.Context, string) (File, error)
	MarkReady(context.Context, string) error
	MarkDeleting(context.Context, string) error
	Delete(context.Context, string) error
	List(context.Context, ListFilter) ([]File, int, error)
	Usage(context.Context) ([]Usage, error)
}

// ObjectStore implementations must support idempotent deletion of missing keys.
type ObjectStore interface {
	Put(context.Context, string, io.ReadSeeker, int64, string) error
	Delete(context.Context, string) error
	DownloadURL(ctx context.Context, key, filename string, ttl time.Duration, inline bool) (string, error)
}
