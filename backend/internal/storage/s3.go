package storage

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type S3 struct {
	client         *s3.Client
	bucket, prefix string
}
type S3Config struct {
	Endpoint, Region, Bucket, Prefix, AccessKey, SecretKey string
	PathStyle                                              bool
}

func NewS3(cfg S3Config) *S3 {
	client := s3.NewFromConfig(aws.Config{Region: cfg.Region, Credentials: credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""), HTTPClient: &http.Client{Timeout: 90 * time.Second}, RetryMaxAttempts: 2}, func(options *s3.Options) {
		if cfg.Endpoint != "" {
			options.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		options.UsePathStyle = cfg.PathStyle
		// 非必需校验不发送额外的分块校验头，兼容常见 S3 服务；对象完整性仍由 HTTPS 保护。
		options.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		options.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})
	return &S3{client: client, bucket: cfg.Bucket, prefix: strings.Trim(cfg.Prefix, "/")}
}

func (s *S3) key(key string) string {
	if s.prefix == "" {
		return key
	}
	return s.prefix + "/" + key
}
func (s *S3) Put(ctx context.Context, key string, reader io.Reader) error {
	contentType := mime.TypeByExtension(path.Ext(key))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{Bucket: &s.bucket, Key: aws.String(s.key(key)), Body: reader, ContentType: &contentType})
	return err
}
func (s *S3) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: aws.String(s.key(key))})
	if err != nil {
		return nil, err
	}
	return result.Body, nil
}
func (s *S3) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &s.bucket, Key: aws.String(s.key(key))})
	var missing *types.NoSuchKey
	if errors.As(err, &missing) {
		return nil
	}
	return err
}
