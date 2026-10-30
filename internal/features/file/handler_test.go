package file

import (
	"bytes"
	"github.com/AtmosferaSoltec/atm-storage-api/internal/auth"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http/httptest"
	"testing"
)

func TestMultipartValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name                string
		parts, size, status int
	}{
		{"valid", 1, 5, 201}, {"extra part", 2, 5, 400}, {"oversize", 1, 1025, 413}, {"empty", 1, 0, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, store := fixture()
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set("auth_claims", auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: owner.Subject}, Role: owner.Role, Tenant: owner.CompanyID, System: owner.System})
			})
			RegisterRoutes(r.Group("/v1"), NewHandler(s, 1024, slog.New(slog.NewTextHandler(io.Discard, nil))))
			var body bytes.Buffer
			w := multipart.NewWriter(&body)
			for i := 0; i < tc.parts; i++ {
				part, err := w.CreateFormFile("file", "test.txt")
				if err != nil {
					t.Fatal(err)
				}
				if _, err = part.Write(bytes.Repeat([]byte("x"), tc.size)); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/v1/companies/"+company+"/files?scope=system&system=olympus&purpose=file", &body)
			req.Header.Set("Content-Type", w.FormDataContentType())
			out := httptest.NewRecorder()
			r.ServeHTTP(out, req)
			if out.Code != tc.status {
				t.Fatalf("got %d: %s", out.Code, out.Body.String())
			}
			if tc.status != 201 && store.puts != 0 {
				t.Fatal("invalid body reached provider")
			}
		})
	}
}
