package file

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"testing"
	"time"
)

const company = "fce664f4-5d66-49d4-a1c1-1c360c2ba49b"

var admin = Principal{Subject: "superadmin", Role: "superadmin"}
var owner = Principal{Subject: "owner", Role: "user", CompanyID: company, System: "olympus"}

type memoryRepo struct {
	files      map[string]File
	readyError bool
}

func (r *memoryRepo) Create(_ context.Context, f File) error { r.files[f.ID] = f; return nil }
func (r *memoryRepo) Get(_ context.Context, id string) (File, error) {
	f, ok := r.files[id]
	if !ok {
		return File{}, ErrNotFound
	}
	return f, nil
}
func (r *memoryRepo) GetMany(_ context.Context, ids []string) ([]File, error) {
	var out []File
	for _, id := range ids {
		if f, ok := r.files[id]; ok {
			out = append(out, f)
		}
	}
	return out, nil
}
func (r *memoryRepo) MarkReady(_ context.Context, id string) error {
	if r.readyError {
		return errors.New("database unavailable")
	}
	f := r.files[id]
	f.State = "ready"
	r.files[id] = f
	return nil
}
func (r *memoryRepo) MarkDeleting(_ context.Context, id string) error {
	f := r.files[id]
	f.State = "deleting"
	r.files[id] = f
	return nil
}
func (r *memoryRepo) Delete(_ context.Context, id string) error { delete(r.files, id); return nil }
func (r *memoryRepo) List(_ context.Context, f ListFilter) ([]File, int, error) {
	items := []File{}
	for _, file := range r.files {
		if file.State == "ready" && (f.CompanyID == "" || f.CompanyID == file.CompanyID) && (f.System == "" || f.System == file.System) {
			items = append(items, file)
		}
	}
	return items, len(items), nil
}
func (r *memoryRepo) Usage(context.Context) ([]Usage, error) {
	var total int64
	for _, file := range r.files {
		if file.State == "ready" {
			total += file.Size
		}
	}
	return []Usage{{Scope: "system", System: "olympus", FilesCount: int64(len(r.files)), TotalBytes: total}}, nil
}

type fakeStore struct {
	putError, deleteError bool
	puts, deletes         int
}

func (s *fakeStore) Put(_ context.Context, _ string, _ io.ReadSeeker, _ int64, _ string) error {
	s.puts++
	if s.putError {
		return errors.New("upstream failed")
	}
	return nil
}
func (s *fakeStore) Delete(_ context.Context, _ string) error {
	s.deletes++
	if s.deleteError {
		return errors.New("upstream failed")
	}
	return nil
}
func (s *fakeStore) DownloadURL(_ context.Context, _, _ string, _ time.Duration, _ bool) (string, error) {
	return "https://example.test/signed", nil
}
func fixture() (*Service, *memoryRepo, *fakeStore) {
	r := &memoryRepo{files: map[string]File{}}
	p := &fakeStore{}
	return NewService(r, p, Settings{Environment: "development", Provider: "r2", Bucket: "atm-dev", MaxUploadBytes: 1024, MaxLogoBytes: 512, DownloadTTL: time.Minute, PreviewTTL: time.Hour}), r, p
}
func uploadInput() Upload {
	return Upload{CompanyID: company, System: "olympus", Scope: "system", Purpose: "file", Filename: "invoice.txt", ContentType: "text/plain", Size: 5}
}
func TestUploadAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    Principal
		u    Upload
		err  error
	}{
		{"own system", owner, uploadInput(), nil},
		{"other company", Principal{Subject: "owner", Role: "user", CompanyID: "other", System: "olympus"}, uploadInput(), ErrForbidden},
		{"other system", Principal{Subject: "owner", Role: "user", CompanyID: company, System: "vendimia"}, uploadInput(), ErrForbidden},
		{"shared company", owner, Upload{CompanyID: company, Scope: "company", Purpose: "logo"}, ErrForbidden},
		{"admin logo", admin, Upload{CompanyID: company, Scope: "company", Purpose: "logo"}, nil},
		{"path injection", admin, Upload{CompanyID: company, System: "../escape", Scope: "system", Purpose: "file"}, ErrInvalid},
		{"system logo", admin, Upload{CompanyID: company, System: "olympus", Scope: "system", Purpose: "logo"}, ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _ := fixture()
			if err := s.ValidateUpload(tc.p, tc.u); !errors.Is(err, tc.err) {
				t.Fatalf("got %v want %v", err, tc.err)
			}
		})
	}
}
func TestPrivateFileIsolation(t *testing.T) {
	s, _, store := fixture()
	ctx := context.Background()
	f, err := s.Upload(ctx, owner, uploadInput(), bytes.NewReader([]byte("hello")))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []Principal{{Subject: "other", Role: "user", CompanyID: company, System: "olympus"}, {Subject: "owner", Role: "user", CompanyID: company, System: "vendimia"}, {Subject: "owner", Role: "user", CompanyID: "other", System: "olympus"}} {
		if _, err := s.Get(ctx, p, f.ID); !errors.Is(err, ErrNotFound) {
			t.Fatal("metadata leaked", err)
		}
		if _, err := s.Download(ctx, p, f.ID); !errors.Is(err, ErrNotFound) {
			t.Fatal("download leaked", err)
		}
		if err := s.Delete(ctx, p, f.ID); !errors.Is(err, ErrNotFound) {
			t.Fatal("unauthorized deletion", err)
		}
	}
	if store.deletes != 0 {
		t.Fatal("unauthorized provider access")
	}
	if _, err = s.Download(ctx, owner, f.ID); err != nil {
		t.Fatal(err)
	}
}
func TestCompanyAssetSharedAcrossSystems(t *testing.T) {
	s, _, _ := fixture()
	ctx := context.Background()
	u := uploadInput()
	u.Scope = "company"
	u.System = ""
	f, err := s.Upload(ctx, admin, u, bytes.NewReader([]byte("hello")))
	if err != nil {
		t.Fatal(err)
	}
	reader := Principal{Subject: "reader", Role: "user", CompanyID: company, System: "vendimia"}
	if _, err = s.Get(ctx, reader, f.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(ctx, reader, f.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
func TestUploadFailureNeverExposesPendingFile(t *testing.T) {
	for _, dbFailure := range []bool{false, true} {
		s, r, p := fixture()
		p.putError = !dbFailure
		r.readyError = dbFailure
		if _, err := s.Upload(context.Background(), owner, uploadInput(), bytes.NewReader([]byte("hello"))); err == nil {
			t.Fatal("expected failure")
		}
		if len(r.files) != 1 {
			t.Fatal("pending metadata must be retained for reconciliation")
		}
		for id, f := range r.files {
			if f.State != "pending" {
				t.Fatal(f.State)
			}
			if _, err := s.Get(context.Background(), admin, id); !errors.Is(err, ErrNotFound) {
				t.Fatal("pending file exposed")
			}
		}
	}
}
func TestDeletionRetryAndProviderBinding(t *testing.T) {
	s, r, p := fixture()
	ctx := context.Background()
	f, err := s.Upload(ctx, owner, uploadInput(), bytes.NewReader([]byte("hello")))
	if err != nil {
		t.Fatal(err)
	}
	s.settings.Bucket = "different-dev"
	if err = s.Delete(ctx, owner, f.ID); !errors.Is(err, ErrProvider) || p.deletes != 0 {
		t.Fatal("must not delete from changed bucket", err)
	}
	s.settings.Bucket = "atm-dev"
	p.deleteError = true
	if err = s.Delete(ctx, owner, f.ID); !errors.Is(err, ErrProvider) {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, owner, f.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleting file exposed")
	}
	p.deleteError = false
	if err = s.Delete(ctx, owner, f.ID); err != nil {
		t.Fatal(err)
	}
	if len(r.files) != 0 {
		t.Fatal("metadata retained after deletion")
	}
}
func TestLogoValidationAndSize(t *testing.T) {
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, contentType string
		body              []byte
		size              int64
		want              error
	}{
		{"png", "image/png", imageBytes.Bytes(), int64(imageBytes.Len()), nil},
		{"fake png", "image/png", []byte("not an image"), 12, ErrMediaType},
		{"svg", "image/svg+xml", []byte("<svg/>"), 6, ErrMediaType},
		{"oversize", "image/png", imageBytes.Bytes(), 513, ErrTooLarge},
		{"empty", "image/png", nil, 0, ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, p := fixture()
			u := Upload{CompanyID: company, Scope: "company", Purpose: "logo", Filename: "logo.png", ContentType: tc.contentType, Size: tc.size}
			_, err := s.Upload(context.Background(), admin, u, bytes.NewReader(tc.body))
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			if tc.want != nil && p.puts != 0 {
				t.Fatal("invalid file reached provider")
			}
		})
	}
}
func TestListAndSummaryAreSuperadminOnly(t *testing.T) {
	s, _, _ := fixture()
	ctx := context.Background()
	if _, err := s.Upload(ctx, owner, uploadInput(), bytes.NewReader([]byte("hello"))); err != nil {
		t.Fatal(err)
	}
	if _, err := s.List(ctx, owner, ListFilter{}); !errors.Is(err, ErrForbidden) {
		t.Fatal("user listed files", err)
	}
	if _, err := s.Summary(ctx, owner); !errors.Is(err, ErrForbidden) {
		t.Fatal("user read usage", err)
	}
	page, err := s.List(ctx, admin, ListFilter{CompanyID: company})
	if err != nil || page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("got %+v, %v", page, err)
	}
	summary, err := s.Summary(ctx, admin)
	if err != nil || summary.FilesCount != 1 || summary.TotalBytes != 5 {
		t.Fatalf("got %+v, %v", summary, err)
	}
}
func TestListRejectsInvalidFilters(t *testing.T) {
	s, _, _ := fixture()
	for name, f := range map[string]ListFilter{
		"company": {CompanyID: "nope"}, "scope": {Scope: "all"}, "system": {System: "../x"}, "limit": {Limit: 1000}, "offset": {Limit: 10, Offset: -1},
	} {
		if _, err := s.List(context.Background(), admin, f); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: got %v", name, err)
		}
	}
}
func TestPreviewOnlyForImages(t *testing.T) {
	s, _, _ := fixture()
	ctx := context.Background()
	text, err := s.Upload(ctx, owner, uploadInput(), bytes.NewReader([]byte("hello")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Preview(ctx, owner, text.ID); !errors.Is(err, ErrNotPreviewable) {
		t.Fatalf("text previewed: %v", err)
	}
	var buf bytes.Buffer
	if err = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	logoUpload := Upload{CompanyID: company, Scope: "company", Purpose: "logo", Filename: "logo.png", ContentType: "image/png", Size: int64(buf.Len())}
	logo, err := s.Upload(ctx, admin, logoUpload, bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Preview(ctx, owner, logo.ID); err != nil {
		t.Fatalf("company member could not preview the shared logo: %v", err)
	}
	if _, err = s.Preview(ctx, Principal{Subject: "x", Role: "user", CompanyID: "other"}, logo.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other company previewed the logo: %v", err)
	}
}
func TestServiceTokenReadsOnlyCompanyLogos(t *testing.T) {
	s, _, store := fixture()
	ctx := context.Background()
	service := Principal{Subject: "system:olympus", Role: "service", System: "olympus"}
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	logo, err := s.Upload(ctx, admin, Upload{CompanyID: company, Scope: "company", Purpose: "logo", Filename: "logo.png", ContentType: "image/png", Size: int64(buf.Len())}, bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	private, err := s.Upload(ctx, owner, uploadInput(), bytes.NewReader([]byte("hello")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, service, logo.ID); err != nil {
		t.Fatalf("service could not read a company logo: %v", err)
	}
	if _, err = s.Download(ctx, service, logo.ID); err != nil {
		t.Fatalf("service could not download a company logo: %v", err)
	}
	if _, err = s.Preview(ctx, service, logo.ID); err != nil {
		t.Fatalf("service could not preview a company logo: %v", err)
	}
	// Another system's backend reads neither the private file of this system nor, of course, writes it.
	stranger := Principal{Subject: "system:vendimia", Role: "service", System: "vendimia"}
	if _, err = s.Get(ctx, stranger, private.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("service of another system read a private system file: %v", err)
	}
	if err = s.Delete(ctx, service, logo.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("service deleted a logo: %v", err)
	}
	if err = s.Delete(ctx, stranger, private.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("service of another system deleted a private file: %v", err)
	}
	if store.deletes != 0 {
		t.Fatal("service touched the provider on a denied deletion")
	}
	if err = s.ValidateUpload(stranger, uploadInput()); !errors.Is(err, ErrForbidden) {
		t.Fatalf("service uploaded a file of another system: %v", err)
	}
	if err = s.ValidateUpload(service, Upload{CompanyID: company, Scope: "company", Purpose: "logo"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("service replaced a logo: %v", err)
	}
	if _, err = s.List(ctx, service, ListFilter{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("service listed files: %v", err)
	}
	if _, err = s.Summary(ctx, service); !errors.Is(err, ErrForbidden) {
		t.Fatalf("service read usage: %v", err)
	}
}

// A system's backend stores and reads the files of its own system, for the company it names.
func TestServiceTokenOwnSystemFiles(t *testing.T) {
	s, _, store := fixture()
	ctx := context.Background()
	backend := Principal{Subject: "system:olympus", Role: "service", System: "olympus"}
	otherBackend := Principal{Subject: "system:vendimia", Role: "service", System: "vendimia"}

	f, err := s.Upload(ctx, backend, uploadInput(), bytes.NewReader([]byte("hello")))
	if err != nil {
		t.Fatalf("service could not store a file of its system: %v", err)
	}
	if f.System != "olympus" || f.Scope != "system" {
		t.Fatalf("got %+v", f)
	}
	if _, err = s.Get(ctx, backend, f.ID); err != nil {
		t.Fatalf("service could not read its own file: %v", err)
	}
	if _, err = s.Download(ctx, backend, f.ID); err != nil {
		t.Fatalf("service could not download its own file: %v", err)
	}

	for name, call := range map[string]func() error{
		"read another system's file":   func() error { _, err := s.Get(ctx, otherBackend, f.ID); return err },
		"delete another system's file": func() error { return s.Delete(ctx, otherBackend, f.ID) },
	} {
		if err := call(); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s: got %v", name, err)
		}
	}
	if store.deletes != 0 {
		t.Fatal("a denied deletion touched the provider")
	}

	// It may not write into another system, nor company assets.
	other := uploadInput()
	other.System = "vendimia"
	if _, err = s.Upload(ctx, backend, other, bytes.NewReader([]byte("hello"))); !errors.Is(err, ErrForbidden) {
		t.Fatalf("service wrote into another system: %v", err)
	}
	if err = s.ValidateUpload(backend, Upload{CompanyID: company, Scope: "company", Purpose: "logo"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("service replaced a company logo: %v", err)
	}

	if err = s.Delete(ctx, backend, f.ID); err != nil {
		t.Fatalf("service could not delete its own file: %v", err)
	}
	if store.deletes != 1 {
		t.Fatalf("deletes %d", store.deletes)
	}
	if _, err = s.List(ctx, backend, ListFilter{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("service listed files: %v", err)
	}
}

func TestPreviewMany(t *testing.T) {
	s, _, _ := fixture()
	ctx := context.Background()
	backend := Principal{Subject: "system:olympus", Role: "service", System: "olympus"}
	pngBody := func() ([]byte, int64) {
		var buf bytes.Buffer
		if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes(), int64(buf.Len())
	}
	data, size := pngBody()
	photoInput := Upload{CompanyID: company, System: "olympus", Scope: "system", Purpose: "file", Filename: "foto.png", ContentType: "image/png", Size: size}
	photo, err := s.Upload(ctx, backend, photoInput, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	text, err := s.Upload(ctx, backend, uploadInput(), bytes.NewReader([]byte("hello")))
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := s.Upload(ctx, admin, Upload{CompanyID: company, System: "vendimia", Scope: "system", Purpose: "file", Filename: "x.png", ContentType: "image/png", Size: size}, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	links, err := s.PreviewMany(ctx, backend, []string{photo.ID, text.ID, foreign.ID, photo.ID, "00000000-0000-4000-8000-000000000000"})
	if err != nil {
		t.Fatal(err)
	}
	// Only the readable image gets a link; the rest are left out instead of failing the batch.
	if len(links) != 1 || links[photo.ID].URL == "" {
		t.Fatalf("got %+v", links)
	}
	if got := time.Until(links[photo.ID].ExpiresAt); got < 59*time.Minute || got > time.Hour {
		t.Fatalf("a preview link must live the preview ttl, got %v", got)
	}

	for name, ids := range map[string][]string{"empty": {}, "not a uuid": {"nope"}, "too many": make([]string, 101)} {
		if _, err := s.PreviewMany(ctx, backend, ids); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: got %v", name, err)
		}
	}
}
