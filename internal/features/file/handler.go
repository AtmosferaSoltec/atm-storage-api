package file

import (
	"errors"
	"github.com/AtmosferaSoltec/atm-storage-api/internal/auth"
	"github.com/AtmosferaSoltec/atm-storage-api/internal/platform/httpserver"
	"github.com/gin-gonic/gin"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
)

type Handler struct {
	service  *Service
	maxBytes int64
	log      *slog.Logger
}

func NewHandler(service *Service, maxBytes int64, log *slog.Logger) *Handler {
	return &Handler{service: service, maxBytes: maxBytes, log: log}
}
func RegisterRoutes(group *gin.RouterGroup, h *Handler) {
	group.POST("/companies/:company_id/files", h.Upload)
	group.GET("/files", h.List)
	group.GET("/files/summary", h.Summary)
	group.GET("/files/:id", h.Get)
	group.GET("/files/:id/download", h.Download)
	group.GET("/files/:id/preview", h.Preview)
	group.DELETE("/files/:id", h.Delete)
}
func principal(c *gin.Context) Principal {
	p := auth.FromContext(c)
	return Principal{Subject: p.Subject, Role: p.Role, CompanyID: p.Tenant, System: p.System}
}
func (h *Handler) fail(c *gin.Context, err error) {
	status, code, message := 500, "internal_error", "Unable to complete the request."
	switch {
	case errors.Is(err, ErrInvalid):
		status, code, message = 400, "invalid_input", "Invalid upload parameters."
	case errors.Is(err, ErrForbidden):
		status, code, message = 403, "forbidden", "Insufficient permissions."
	case errors.Is(err, ErrNotFound):
		status, code, message = 404, "file_not_found", "File not found."
	case errors.Is(err, ErrTooLarge):
		status, code, message = 413, "file_too_large", "File exceeds the upload limit."
	case errors.Is(err, ErrMediaType):
		status, code, message = 415, "unsupported_media_type", "Logo must be a valid PNG or JPEG image."
	case errors.Is(err, ErrNotPreviewable):
		status, code, message = 415, "not_previewable", "This file type cannot be previewed."
	case errors.Is(err, ErrProvider):
		status, code, message = 503, "provider_unavailable", "Storage provider unavailable."
	}
	// Avoid logging provider URLs, credentials, request bodies or database DSNs.
	if status >= 500 {
		h.log.Error("file operation failed", "code", code, "request_id", c.GetHeader("X-Request-ID"))
	}
	httpserver.Error(c, status, code, message)
}
func (h *Handler) Upload(c *gin.Context) {
	u := Upload{CompanyID: c.Param("company_id"), System: c.Query("system"), Scope: c.Query("scope"), Purpose: c.Query("purpose")}
	for key := range c.Request.URL.Query() {
		if key != "scope" && key != "purpose" && key != "system" {
			h.fail(c, ErrInvalid)
			return
		}
	}
	if err := h.service.ValidateUpload(principal(c), u); err != nil {
		h.fail(c, err)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.maxBytes+(64<<10))
	reader, err := c.Request.MultipartReader()
	if err != nil {
		h.fail(c, ErrInvalid)
		return
	}
	part, err := reader.NextPart()
	if err != nil {
		h.uploadError(c, err)
		return
	}
	defer part.Close()
	if part.FormName() != "file" || part.FileName() == "" {
		h.fail(c, ErrInvalid)
		return
	}
	temp, err := os.CreateTemp("", "atm-storage-*")
	if err != nil {
		h.fail(c, err)
		return
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	size, err := io.Copy(temp, io.LimitReader(part, h.maxBytes+1))
	if size > h.maxBytes {
		h.fail(c, ErrTooLarge)
		return
	}
	if err != nil {
		h.uploadError(c, err)
		return
	}
	if _, err = reader.NextPart(); err != io.EOF {
		if err == nil {
			err = ErrInvalid
		}
		h.uploadError(c, err)
		return
	}
	if _, err = temp.Seek(0, io.SeekStart); err != nil {
		h.fail(c, err)
		return
	}
	header := make([]byte, 512)
	n, err := temp.Read(header)
	if err != nil && err != io.EOF {
		h.fail(c, err)
		return
	}
	u.Filename = part.FileName()
	u.Size = size
	u.ContentType = http.DetectContentType(header[:n])
	if _, err = temp.Seek(0, io.SeekStart); err != nil {
		h.fail(c, err)
		return
	}
	f, err := h.service.Upload(c.Request.Context(), principal(c), u, temp)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.Header("Location", "/v1/files/"+f.ID)
	c.JSON(201, f)
}
func (h *Handler) uploadError(c *gin.Context, err error) {
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		h.fail(c, ErrTooLarge)
	} else {
		h.fail(c, ErrInvalid)
	}
}
func (h *Handler) Get(c *gin.Context) {
	f, err := h.service.Get(c.Request.Context(), principal(c), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(200, f)
}
func (h *Handler) Download(c *gin.Context) {
	d, err := h.service.Download(c.Request.Context(), principal(c), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, d)
}
func (h *Handler) Delete(c *gin.Context) {
	if err := h.service.Delete(c.Request.Context(), principal(c), c.Param("id")); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(204)
}
func (h *Handler) List(c *gin.Context) {
	f := ListFilter{CompanyID: c.Query("company_id"), System: c.Query("system"), Scope: c.Query("scope")}
	for key := range c.Request.URL.Query() {
		if key != "company_id" && key != "system" && key != "scope" && key != "limit" && key != "offset" {
			h.fail(c, ErrInvalid)
			return
		}
	}
	for key, dest := range map[string]*int{"limit": &f.Limit, "offset": &f.Offset} {
		if raw := c.Query(key); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil {
				h.fail(c, ErrInvalid)
				return
			}
			*dest = n
		}
	}
	page, err := h.service.List(c.Request.Context(), principal(c), f)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(200, page)
}
func (h *Handler) Summary(c *gin.Context) {
	summary, err := h.service.Summary(c.Request.Context(), principal(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(200, summary)
}
func (h *Handler) Preview(c *gin.Context) {
	d, err := h.service.Preview(c.Request.Context(), principal(c), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, d)
}
