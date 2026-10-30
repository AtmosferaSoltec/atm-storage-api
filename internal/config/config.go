package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment, HTTPPort, DatabaseURL, Provider, Bucket, Endpoint, Region, AccessKeyID, SecretAccessKey, AuthPublicKeyBase64, AuthIssuer string
	MaxUploadBytes, MaxLogoBytes                                                                                                          int64
	DownloadTTL, PreviewTTL                                                                                                               time.Duration
	CORSOrigins                                                                                                                           []string
}

func Load() (Config, error) {
	c := Config{Environment: value("APP_ENV", "development"), HTTPPort: value("HTTP_PORT", "8084"), DatabaseURL: os.Getenv("DATABASE_URL"), Provider: value("STORAGE_PROVIDER", "r2"), Bucket: os.Getenv("STORAGE_BUCKET"), Endpoint: os.Getenv("STORAGE_ENDPOINT"), Region: value("STORAGE_REGION", "auto"), AccessKeyID: os.Getenv("STORAGE_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("STORAGE_SECRET_ACCESS_KEY"), AuthPublicKeyBase64: os.Getenv("AUTH_PUBLIC_KEY_BASE64"), AuthIssuer: os.Getenv("AUTH_ISSUER"), MaxUploadBytes: 25 << 20, MaxLogoBytes: 2 << 20, DownloadTTL: 5 * time.Minute, PreviewTTL: time.Hour}
	for name, dest := range map[string]*int64{"MAX_UPLOAD_BYTES": &c.MaxUploadBytes, "MAX_LOGO_BYTES": &c.MaxLogoBytes} {
		if raw := os.Getenv(name); raw != "" {
			v, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return Config{}, fmt.Errorf("invalid %s", name)
			}
			*dest = v
		}
	}
	if raw := os.Getenv("DOWNLOAD_URL_TTL"); raw != "" {
		v, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, errors.New("invalid DOWNLOAD_URL_TTL")
		}
		c.DownloadTTL = v
	}
	if raw := os.Getenv("PREVIEW_URL_TTL"); raw != "" {
		v, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, errors.New("invalid PREVIEW_URL_TTL")
		}
		c.PreviewTTL = v
	}
	for _, origin := range strings.Split(os.Getenv("WEB_CORS_ORIGINS"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			c.CORSOrigins = append(c.CORSOrigins, origin)
		}
	}
	return c, c.Validate()
}
func (c Config) Validate() error {
	suffix := "-dev"
	switch c.Environment {
	case "development":
	case "production":
		suffix = "-prod"
	default:
		return errors.New("APP_ENV must be development or production")
	}
	if !strings.HasSuffix(c.Bucket, suffix) {
		return fmt.Errorf("STORAGE_BUCKET must end with %s for this environment", suffix)
	}
	if c.Provider != "r2" && c.Provider != "s3" {
		return errors.New("STORAGE_PROVIDER must be r2 or s3")
	}
	for name, v := range map[string]string{"DATABASE_URL": c.DatabaseURL, "STORAGE_ACCESS_KEY_ID": c.AccessKeyID, "STORAGE_SECRET_ACCESS_KEY": c.SecretAccessKey, "AUTH_PUBLIC_KEY_BASE64": c.AuthPublicKeyBase64, "AUTH_ISSUER": c.AuthIssuer, "STORAGE_REGION": c.Region} {
		if v == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if c.Provider == "r2" && (c.Endpoint == "" || c.Region != "auto") {
		return errors.New("R2 requires STORAGE_ENDPOINT and STORAGE_REGION=auto")
	}
	if c.Endpoint != "" {
		u, err := url.Parse(c.Endpoint)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(c.Environment == "development" && u.Scheme == "http")) {
			return errors.New("invalid STORAGE_ENDPOINT; production requires HTTPS")
		}
	}
	port, err := strconv.Atoi(c.HTTPPort)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("invalid HTTP_PORT")
	}
	if c.MaxUploadBytes < 1 || c.MaxUploadBytes > 100<<20 || c.MaxLogoBytes < 1 || c.MaxLogoBytes > c.MaxUploadBytes {
		return errors.New("upload limits must be positive, logo <= upload <= 100 MiB")
	}
	if c.DownloadTTL < time.Second || c.DownloadTTL > time.Hour {
		return errors.New("DOWNLOAD_URL_TTL must be between 1s and 1h")
	}
	if c.PreviewTTL < time.Second || c.PreviewTTL > 24*time.Hour {
		return errors.New("PREVIEW_URL_TTL must be between 1s and 24h")
	}
	for _, origin := range c.CORSOrigins {
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(c.Environment == "development" && u.Scheme == "http")) {
			return errors.New("invalid WEB_CORS_ORIGINS")
		}
	}
	return nil
}
func value(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
