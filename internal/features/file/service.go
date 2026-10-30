package file

import (
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

var systemPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type Service struct {
	repo     Repository
	store    ObjectStore
	settings Settings
}

func NewService(repo Repository, store ObjectStore, settings Settings) *Service {
	return &Service{repo: repo, store: store, settings: settings}
}
func (s *Service) ValidateUpload(p Principal, u Upload) error {
	id, err := uuid.Parse(u.CompanyID)
	if err != nil || id == uuid.Nil || id.String() != u.CompanyID {
		return ErrInvalid
	}
	if u.Scope != "company" && u.Scope != "system" {
		return ErrInvalid
	}
	if u.Purpose != "logo" && u.Purpose != "file" {
		return ErrInvalid
	}
	if u.Scope == "company" && u.System != "" {
		return ErrInvalid
	}
	if u.Scope == "system" && (!systemPattern.MatchString(u.System) || u.Purpose == "logo") {
		return ErrInvalid
	}
	if p.Role == "superadmin" {
		return nil
	}
	if p.Role != "user" || p.Subject == "" || p.CompanyID != u.CompanyID || p.System != u.System || u.Scope != "system" {
		return ErrForbidden
	}
	return nil
}
func (s *Service) Upload(ctx context.Context, p Principal, u Upload, body io.ReadSeeker) (File, error) {
	if err := s.ValidateUpload(p, u); err != nil {
		return File{}, err
	}
	if u.Size <= 0 {
		return File{}, ErrInvalid
	}
	if u.Size > s.settings.MaxUploadBytes || (u.Purpose == "logo" && u.Size > s.settings.MaxLogoBytes) {
		return File{}, ErrTooLarge
	}
	name := filepath.Base(strings.ReplaceAll(u.Filename, `\`, "/"))
	if name == "." || name == "/" || name == "" || len(name) > 255 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return File{}, ErrInvalid
	}
	if u.Purpose == "logo" {
		if u.ContentType != "image/png" && u.ContentType != "image/jpeg" {
			return File{}, ErrMediaType
		}
		config, _, err := image.DecodeConfig(body)
		if err != nil || config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > 16000000 {
			return File{}, ErrMediaType
		}
		if _, err = body.Seek(0, io.SeekStart); err != nil {
			return File{}, err
		}
		if _, _, err = image.Decode(body); err != nil {
			return File{}, ErrMediaType
		}
	}
	if _, err := body.Seek(0, io.SeekStart); err != nil {
		return File{}, err
	}
	f := File{ID: uuid.NewString(), CompanyID: u.CompanyID, System: u.System, Scope: u.Scope, Purpose: u.Purpose, Filename: name, ContentType: u.ContentType, Size: u.Size, CreatedAt: time.Now().UTC(), OwnerID: p.Subject, State: "pending", Provider: s.settings.Provider, Bucket: s.settings.Bucket}
	namespace := "shared"
	if f.Scope == "system" {
		namespace = f.System
	}
	f.Key = fmt.Sprintf("%s/companies/%s/%s/%s/%s", s.settings.Environment, f.CompanyID, f.Scope, namespace, f.ID)
	if err := s.repo.Create(ctx, f); err != nil {
		return File{}, fmt.Errorf("create file metadata: %w", err)
	}
	if err := s.store.Put(ctx, f.Key, body, f.Size, f.ContentType); err != nil {
		return File{}, ErrProvider
	}
	if err := s.repo.MarkReady(ctx, f.ID); err != nil {
		return File{}, fmt.Errorf("complete upload: %w", err)
	}
	f.State = "ready"
	return f, nil
}
func canAccess(p Principal, f File, write bool) bool {
	if p.Role == "superadmin" {
		return true
	}
	// A system's backend (service token) may only read company logos, to print them on its documents. The token
	// names the system, not a company, so it never reaches system files, uploads, deletions or listings.
	if p.Role == "service" {
		return !write && f.Scope == "company" && f.Purpose == "logo"
	}
	if p.Role != "user" || p.CompanyID != f.CompanyID {
		return false
	}
	if f.Scope == "company" {
		return !write
	}
	return p.System == f.System && p.Subject == f.OwnerID
}
func (s *Service) Get(ctx context.Context, p Principal, id string) (File, error) {
	if _, err := uuid.Parse(id); err != nil {
		return File{}, ErrNotFound
	}
	f, err := s.repo.Get(ctx, id)
	if err != nil {
		return File{}, err
	}
	if f.State != "ready" || !canAccess(p, f, false) {
		return File{}, ErrNotFound
	}
	return f, nil
}
func (s *Service) Download(ctx context.Context, p Principal, id string) (Download, error) {
	return s.signedURL(ctx, p, id, false)
}

// previewable lists the types a browser may render inline. Anything that can run script (SVG, HTML) is excluded.
var previewable = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// Preview returns a short-lived URL that displays the image in the browser instead of downloading it.
func (s *Service) Preview(ctx context.Context, p Principal, id string) (Download, error) {
	return s.signedURL(ctx, p, id, true)
}
func (s *Service) signedURL(ctx context.Context, p Principal, id string, inline bool) (Download, error) {
	f, err := s.Get(ctx, p, id)
	if err != nil {
		return Download{}, err
	}
	if inline && !previewable[f.ContentType] {
		return Download{}, ErrNotPreviewable
	}
	if f.Provider != s.settings.Provider || f.Bucket != s.settings.Bucket {
		return Download{}, ErrProvider
	}
	expires := time.Now().UTC().Add(s.settings.DownloadTTL)
	url, err := s.store.DownloadURL(ctx, f.Key, f.Filename, s.settings.DownloadTTL, inline)
	if err != nil {
		return Download{}, ErrProvider
	}
	return Download{URL: url, ExpiresAt: expires}, nil
}
func (s *Service) Delete(ctx context.Context, p Principal, id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrNotFound
	}
	f, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if (f.State != "ready" && f.State != "deleting") || !canAccess(p, f, true) {
		return ErrNotFound
	}
	if f.Provider != s.settings.Provider || f.Bucket != s.settings.Bucket {
		return ErrProvider
	}
	if err = s.repo.MarkDeleting(ctx, id); err != nil {
		return err
	}
	if err = s.store.Delete(ctx, f.Key); err != nil {
		return ErrProvider
	}
	return s.repo.Delete(ctx, id)
}

const (
	defaultPageSize = 25
	maxPageSize     = 100
)

// List and Summary are administrative views across companies, so only superadmin may call them.
func (s *Service) List(ctx context.Context, p Principal, f ListFilter) (Page, error) {
	if p.Role != "superadmin" {
		return Page{}, ErrForbidden
	}
	if f.CompanyID != "" {
		if id, err := uuid.Parse(f.CompanyID); err != nil || id.String() != f.CompanyID {
			return Page{}, ErrInvalid
		}
	}
	if f.Scope != "" && f.Scope != "company" && f.Scope != "system" {
		return Page{}, ErrInvalid
	}
	if f.System != "" && !systemPattern.MatchString(f.System) {
		return Page{}, ErrInvalid
	}
	if f.Limit == 0 {
		f.Limit = defaultPageSize
	}
	if f.Limit < 1 || f.Limit > maxPageSize || f.Offset < 0 {
		return Page{}, ErrInvalid
	}
	items, total, err := s.repo.List(ctx, f)
	if err != nil {
		return Page{}, fmt.Errorf("list files: %w", err)
	}
	return Page{Items: items, Total: total}, nil
}
func (s *Service) Summary(ctx context.Context, p Principal) (Summary, error) {
	if p.Role != "superadmin" {
		return Summary{}, ErrForbidden
	}
	usage, err := s.repo.Usage(ctx)
	if err != nil {
		return Summary{}, fmt.Errorf("summarize usage: %w", err)
	}
	summary := Summary{Usage: usage}
	for _, u := range usage {
		summary.FilesCount += u.FilesCount
		summary.TotalBytes += u.TotalBytes
	}
	return summary, nil
}
