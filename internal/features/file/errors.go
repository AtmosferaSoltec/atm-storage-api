package file

import "errors"

var (
	ErrInvalid        = errors.New("invalid_input")
	ErrForbidden      = errors.New("forbidden")
	ErrNotFound       = errors.New("file_not_found")
	ErrTooLarge       = errors.New("file_too_large")
	ErrMediaType      = errors.New("unsupported_media_type")
	ErrProvider       = errors.New("provider_unavailable")
	ErrNotPreviewable = errors.New("not_previewable")
)
