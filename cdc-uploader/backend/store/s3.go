package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const PresignTTL = 15 * time.Minute

type S3Config struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
}

type S3 struct {
	client  *s3.Client
	presign *s3.PresignClient
	bucket  string
}

func NewS3(ctx context.Context, c S3Config) (*S3, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(c.Region),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(c.AccessKey, c.SecretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("s3 config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(c.Endpoint)
		o.UsePathStyle = true
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})

	if _, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(c.Bucket)}); err != nil {
		return nil, fmt.Errorf("s3 bucket %q not reachable (did minio-init run?): %w", c.Bucket, err)
	}

	return &S3{client: client, presign: s3.NewPresignClient(client), bucket: c.Bucket}, nil
}

func (s *S3) CreateMultipart(ctx context.Context, key string) (string, error) {
	out, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return "", fmt.Errorf("create multipart upload: %w", err)
	}
	if out.UploadId == nil {
		return "", errors.New("create multipart upload: no uploadId returned")
	}
	return *out.UploadId, nil
}

func (s *S3) PresignUploadPart(ctx context.Context, key, uploadID string, partNumber int) (string, error) {
	req, err := s.presign.PresignUploadPart(ctx, &s3.UploadPartInput{
		Bucket:     aws.String(s.bucket),
		Key:        aws.String(key),
		UploadId:   aws.String(uploadID),
		PartNumber: aws.Int32(int32(partNumber)),
	}, s3.WithPresignExpires(PresignTTL))
	if err != nil {
		return "", fmt.Errorf("presign part %d: %w", partNumber, err)
	}
	return req.URL, nil
}

type CompletedPart struct {
	N    int
	ETag string
}

func (s *S3) CompleteMultipart(ctx context.Context, key, uploadID string, parts []CompletedPart) error {
	done := make([]types.CompletedPart, len(parts))
	for i, p := range parts {
		done[i] = types.CompletedPart{
			PartNumber: aws.Int32(int32(p.N)),
			ETag:       aws.String(p.ETag),
		}
	}

	_, err := s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:          aws.String(s.bucket),
		Key:             aws.String(key),
		UploadId:        aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{Parts: done},
	})

	if err != nil {
		return fmt.Errorf("complete multipart upload: %w", err)
	}

	return nil
}

func (s *S3) AbortMultipart(ctx context.Context, key, uploadID string) error {
	_, err := s.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(s.bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	if err != nil {
		return fmt.Errorf("abort multipart upload: %w", err)
	}
	return nil
}
