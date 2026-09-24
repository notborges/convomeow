package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type S3Options struct {
	Bucket    string
	Region    string
	Endpoint  string
	AccessKey string
	SecretKey string
}

type S3 struct {
	bucket string
	client *s3.Client
}

func NewS3(ctx context.Context, options S3Options) (*S3, error) {
	if options.Bucket == "" || options.Region == "" {
		return nil, errors.New("S3 bucket and region are required")
	}
	loadOptions := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(options.Region)}
	if options.Endpoint != "" {
		loadOptions = append(loadOptions, awsconfig.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired),
			awsconfig.WithResponseChecksumValidation(aws.ResponseChecksumValidationWhenRequired))
	}
	if options.AccessKey != "" || options.SecretKey != "" {
		if options.AccessKey == "" || options.SecretKey == "" {
			return nil, errors.New("both S3 access keys are required")
		}
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(options.AccessKey, options.SecretKey, "")))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load S3 configuration: %w", err)
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if options.Endpoint != "" {
			o.BaseEndpoint = aws.String(options.Endpoint)
			o.UsePathStyle = true
		}
	})
	return &S3{bucket: options.Bucket, client: client}, nil
}

func (s *S3) Put(ctx context.Context, key string, file *os.File, size int64) error {
	if !validKey(key) || size < 0 {
		return errors.New("invalid media object")
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() != size {
		return errors.New("media size changed before storage")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key),
		Body: file, ContentLength: aws.Int64(size), ContentType: aws.String("application/octet-stream")})
	return err
}

func (s *S3) Open(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	if !validKey(key) {
		return nil, errors.New("invalid media object key")
	}
	input := &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}
	if length >= 0 {
		if offset < 0 || length == 0 {
			return nil, errors.New("invalid media range")
		}
		input.Range = aws.String(fmt.Sprintf("bytes=%d-%d", offset, offset+length-1))
	}
	output, err := s.client.GetObject(ctx, input)
	if err != nil {
		var missing *types.NoSuchKey
		if errors.As(err, &missing) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if length >= 0 && (output.ContentLength == nil || *output.ContentLength != length) {
		output.Body.Close()
		return nil, errors.New("S3 returned an unexpected media range")
	}
	return output.Body, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	if !validKey(key) {
		return errors.New("invalid media object key")
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return err
}
