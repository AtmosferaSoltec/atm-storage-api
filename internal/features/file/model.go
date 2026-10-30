package file

import "time"

type Principal struct{ Subject, Role, CompanyID, System string }
type File struct {
	ID          string    `json:"id"`
	CompanyID   string    `json:"company_id"`
	System      string    `json:"system"`
	Scope       string    `json:"scope"`
	Purpose     string    `json:"purpose"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	CreatedAt   time.Time `json:"created_at"`
	OwnerID     string    `json:"-"`
	State       string    `json:"-"`
	Provider    string    `json:"-"`
	Bucket      string    `json:"-"`
	Key         string    `json:"-"`
}
type Upload struct {
	CompanyID, System, Scope, Purpose, Filename, ContentType string
	Size                                                     int64
}
type Download struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}
type Settings struct {
	Environment, Provider, Bucket string
	MaxUploadBytes, MaxLogoBytes  int64
	// DownloadTTL is how long a download link lives. PreviewTTL is how long an inline image link lives: it is
	// longer because pages keep showing the image after it loaded (a catalog grid left open).
	DownloadTTL, PreviewTTL time.Duration
}

type ListFilter struct {
	CompanyID, System, Scope string
	Limit, Offset            int
}
type Page struct {
	Items []File `json:"items"`
	Total int    `json:"total"`
}
type Usage struct {
	Scope      string `json:"scope"`
	System     string `json:"system"`
	FilesCount int64  `json:"files_count"`
	TotalBytes int64  `json:"total_bytes"`
}
type Summary struct {
	FilesCount int64   `json:"files_count"`
	TotalBytes int64   `json:"total_bytes"`
	Usage      []Usage `json:"usage"`
}
