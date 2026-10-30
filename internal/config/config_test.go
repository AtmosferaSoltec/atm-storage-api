package config

import "testing"

func TestEnvironmentIsolation(t *testing.T) {
	base := Config{Environment: "development", HTTPPort: "8084", DatabaseURL: "postgres://test", Provider: "r2", Bucket: "atm-dev", Endpoint: "https://example.r2.cloudflarestorage.com", Region: "auto", AccessKeyID: "test", SecretAccessKey: "test", AuthPublicKeyBase64: "test", AuthIssuer: "dev", MaxUploadBytes: 1024, MaxLogoBytes: 512, DownloadTTL: 300000000000}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*Config)
	}{
		{"development using production bucket", func(c *Config) { c.Bucket = "atm-prod" }},
		{"production using development bucket", func(c *Config) { c.Environment = "production" }},
		{"unsupported provider", func(c *Config) { c.Provider = "firebase" }},
		{"unknown environment", func(c *Config) { c.Environment = "staging" }},
		{"oversized limit", func(c *Config) { c.MaxUploadBytes = 1 << 40 }},
		{"public CORS wildcard", func(c *Config) { c.CORSOrigins = []string{"*"} }},
		{"production plaintext endpoint", func(c *Config) {
			c.Environment = "production"
			c.Bucket = "atm-prod"
			c.Endpoint = "http://example.test"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := base
			tc.change(&c)
			if err := c.Validate(); err == nil {
				t.Fatal("unsafe configuration accepted")
			}
		})
	}
}
