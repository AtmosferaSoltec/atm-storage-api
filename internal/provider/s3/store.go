// Package s3 adapts S3-compatible object storage, including Cloudflare R2.
package s3

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"io"
	"mime"
	"net/http"
	"time"
)

type Config struct{ Endpoint, Region, AccessKeyID, SecretAccessKey, Bucket string }
type Store struct {
	client    *s3.Client
	presigner *s3.PresignClient
	bucket    string
}

func New(c Config) *Store {
	client := s3.New(s3.Options{Region: c.Region, BaseEndpoint: optional(c.Endpoint), UsePathStyle: true, Credentials: credentials.NewStaticCredentialsProvider(c.AccessKeyID, c.SecretAccessKey, ""), HTTPClient: &http.Client{Timeout: 60 * time.Second}, RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired, ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired})
	return &Store{client: client, presigner: s3.NewPresignClient(client), bucket: c.Bucket}
}
func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func (s *Store) Put(ctx context.Context, key string, body io.ReadSeeker, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{Bucket: &s.bucket, Key: &key, Body: body, ContentLength: &size, ContentType: &contentType})
	return err
}
func (s *Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &s.bucket, Key: &key})
	return err
}
func (s *Store) DownloadURL(ctx context.Context, key, filename string, ttl time.Duration, inline bool) (string, error) {
	kind := "attachment"
	if inline {
		kind = "inline"
	}
	disposition := mime.FormatMediaType(kind, map[string]string{"filename": filename})
	result, err := s.presigner.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key, ResponseContentDisposition: &disposition}, func(o *s3.PresignOptions) { o.Expires = ttl })
	if err != nil {
		return "", err
	}
	return result.URL, nil
}
