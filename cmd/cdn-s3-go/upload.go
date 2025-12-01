package main

import (
	"context"
	"fmt"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func uploadFile() {
	if len(os.Args) < 6 {
		fmt.Fprintf(os.Stderr, "Usage: %s upload <public_bucket_name> <region> <path_to_file> <remote_path>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example: %s upload avatars us ~/ava.jpg /public/user1/a.jpg\n", os.Args[0])
		os.Exit(1)
	}

	readEnv()

	bucketPublicName := os.Args[2]
	regionAlias := os.Args[3]
	localFilePath := os.Args[4]
	remotePath := strings.TrimPrefix(os.Args[5], "/")

	expandedPath := localFilePath
	if strings.HasPrefix(localFilePath, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			slog.Error("failed to expand home directory", "error", err)
			os.Exit(1)
		}
		expandedPath = filepath.Join(homeDir, localFilePath[2:])
	}

	file, err := os.Open(expandedPath)
	if err != nil {
		slog.Error("failed to open file", "error", err)
		os.Exit(1)
	}
	defer file.Close()

	endpoint := os.Getenv(fmt.Sprintf("CDN_BUCKET_ENDPOINT_%s_%s", bucketPublicName, regionAlias))
	region := os.Getenv(fmt.Sprintf("CDN_BUCKET_REGION_%s_%s", bucketPublicName, regionAlias))
	bucketName := os.Getenv(fmt.Sprintf("CDN_BUCKET_NAME_%s_%s", bucketPublicName, regionAlias))
	pathPrefix := os.Getenv(fmt.Sprintf("CDN_BUCKET_PATH_PREFIX_%s_%s", bucketPublicName, regionAlias))
	accessKeyID := os.Getenv(fmt.Sprintf("CDN_BUCKET_ACCESS_KEY_ID_%s_%s", bucketPublicName, regionAlias))
	secretAccessKey := os.Getenv(fmt.Sprintf("CDN_BUCKET_SECRET_ACCESS_KEY_%s_%s", bucketPublicName, regionAlias))

	if endpoint == "" || region == "" || bucketName == "" || accessKeyID == "" || secretAccessKey == "" {
		slog.Error("missing configuration for bucket", "bucket", bucketPublicName, "region", regionAlias)
		os.Exit(1)
	}

	fullPath := remotePath
	if pathPrefix != "" {
		fullPath = strings.TrimPrefix(pathPrefix, "/") + "/" + strings.TrimPrefix(remotePath, "/")
	}

	contentType := mime.TypeByExtension(filepath.Ext(expandedPath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	srv, err := newServer()
	if err != nil {
		slog.Error("failed to initialize server config", "error", err)
		os.Exit(1)
	}

	buckets, ok := srv.buckets[bucketPublicName]
	if !ok {
		slog.Error("bucket not found in configuration", "bucket", bucketPublicName)
		os.Exit(1)
	}

	bucketCfg, ok := buckets[regionAlias]
	if !ok {
		slog.Error("region not found for bucket", "region", regionAlias, "bucket", bucketPublicName)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = bucketCfg.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucketName),
		Key:         aws.String(fullPath),
		Body:        file,
		ContentType: aws.String(contentType),
	})

	if err != nil {
		slog.Error("failed to upload file", "error", err)
		os.Exit(1)
	}

	slog.Info("successfully uploaded file", "local_path", expandedPath, "bucket", bucketName, "remote_path", fullPath, "content_type", contentType)
}
