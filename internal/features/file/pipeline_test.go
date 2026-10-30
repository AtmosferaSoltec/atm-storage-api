package file

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/AtmosferaSoltec/atm-storage-api/internal/auth"
	storage "github.com/AtmosferaSoltec/atm-storage-api/internal/provider/s3"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestStoragePipeline exercises real HTTP handlers, JWT verification, SQL and AWS
// SDK calls. The provider is the local Postman simulation, never a live bucket.
func TestStoragePipeline(t *testing.T) {
	endpoint := os.Getenv("TEST_STORAGE_ENDPOINT")
	dsn := os.Getenv("TEST_DATABASE_URL")
	if endpoint == "" || dsn == "" {
		t.Skip("requires local Postman simulation and disposable PostgreSQL with migrations applied")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	client := &http.Client{Timeout: 10 * time.Second}
	control := func(mode string) {
		t.Helper()
		resp, err := client.Post(endpoint+"/__control/"+mode, "text/plain", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 204 {
			t.Fatalf("control status %d", resp.StatusCode)
		}
	}
	control("reset")
	store := storage.New(storage.Config{Endpoint: endpoint, Region: "auto", AccessKeyID: "simulation-key", SecretAccessKey: "simulation-secret", Bucket: "atm-storage-dev"})
	repo := NewRepository(pool)
	service := NewService(repo, store, Settings{Environment: "development", Provider: "r2", Bucket: "atm-storage-dev", MaxUploadBytes: 1024, MaxLogoBytes: 512, DownloadTTL: time.Minute})
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := auth.New(base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), "storage-test")
	if err != nil {
		t.Fatal(err)
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: owner.Subject, Issuer: "storage-test", IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))}, Role: "user", Tenant: company, System: "olympus"}).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	RegisterRoutes(engine.Group("/v1", verifier.Middleware()), NewHandler(service, 1024, slog.New(slog.NewTextHandler(io.Discard, nil))))
	server := httptest.NewServer(engine)
	defer server.Close()
	request := func(method, path, contentType string, body io.Reader) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, body)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, data
	}
	upload := func() (int, []byte) {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", "proof.txt")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = part.Write([]byte("storage pipeline proof")); err != nil {
			t.Fatal(err)
		}
		if err = writer.Close(); err != nil {
			t.Fatal(err)
		}
		return request("POST", "/v1/companies/"+company+"/files?scope=system&system=olympus&purpose=file", writer.FormDataContentType(), &body)
	}
	status, data := upload()
	if status != 201 {
		t.Fatalf("upload %d %s", status, data)
	}
	var f File
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM storage.files WHERE id=$1`, f.ID)
	status, data = request("GET", "/v1/files/"+f.ID+"/download", "", nil)
	if status != 200 {
		t.Fatalf("download %d %s", status, data)
	}
	var link Download
	if err = json.Unmarshal(data, &link); err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get(link.URL)
	if err != nil {
		t.Fatal(err)
	}
	received, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 || string(received) != "storage pipeline proof" {
		t.Fatalf("downloaded bytes status=%d body=%q err=%v", resp.StatusCode, received, err)
	}
	control("fail/DELETE")
	status, data = request("DELETE", "/v1/files/"+f.ID, "", nil)
	if status != 503 {
		t.Fatalf("failed deletion %d %s", status, data)
	}
	status, _ = request("GET", "/v1/files/"+f.ID+"/download", "", nil)
	if status != 404 {
		t.Fatal("deleting file still accessible", status)
	}
	control("fail/none")
	status, data = request("DELETE", "/v1/files/"+f.ID, "", nil)
	if status != 204 {
		t.Fatalf("retry deletion %d %s", status, data)
	}
	if _, err = repo.Get(ctx, f.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("metadata not deleted", err)
	}
	resp, err = client.Get(link.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatal("object not deleted", resp.StatusCode)
	}
	control("fail/PUT")
	status, data = upload()
	if status != 503 {
		t.Fatalf("failed upload %d %s", status, data)
	}
	// This database is explicitly disposable. Only remove this test's pending rows.
	defer pool.Exec(ctx, `DELETE FROM storage.files WHERE owner_id=$1 AND company_id=$2 AND state='pending'`, owner.Subject, company)
	var pending int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM storage.files WHERE owner_id=$1 AND company_id=$2 AND state='pending'`, owner.Subject, company).Scan(&pending); err != nil || pending < 1 {
		t.Fatal("pending metadata missing", pending, err)
	}
	control("reset")
}
