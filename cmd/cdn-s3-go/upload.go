package main

import (
	"context"
	"fmt"
	"log"
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
			log.Fatalf("[FATA] Failed to expand home directory: %v", err)
		}
		expandedPath = filepath.Join(homeDir, localFilePath[2:])
	}

	file, err := os.Open(expandedPath)
	if err != nil {
		log.Fatalf("[FATA] Failed to open file: %v", err)
	}
	defer file.Close()

	endpoint := os.Getenv(fmt.Sprintf("CDN_BUCKET_ENDPOINT_%s_%s", bucketPublicName, regionAlias))
	region := os.Getenv(fmt.Sprintf("CDN_BUCKET_REGION_%s_%s", bucketPublicName, regionAlias))
	bucketName := os.Getenv(fmt.Sprintf("CDN_BUCKET_NAME_%s_%s", bucketPublicName, regionAlias))
	pathPrefix := os.Getenv(fmt.Sprintf("CDN_BUCKET_PATH_PREFIX_%s_%s", bucketPublicName, regionAlias))
	accessKeyID := os.Getenv(fmt.Sprintf("CDN_BUCKET_ACCESS_KEY_ID_%s_%s", bucketPublicName, regionAlias))
	secretAccessKey := os.Getenv(fmt.Sprintf("CDN_BUCKET_SECRET_ACCESS_KEY_%s_%s", bucketPublicName, regionAlias))

	if endpoint == "" || region == "" || bucketName == "" || accessKeyID == "" || secretAccessKey == "" {
		log.Fatalf("[FATA] Missing configuration for bucket '%s' in region '%s'", bucketPublicName, regionAlias)
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
		log.Fatalf("[FATA] Failed to initialize server config: %v", err)
	}

	buckets, ok := srv.buckets[bucketPublicName]
	if !ok {
		log.Fatalf("[FATA] Bucket '%s' not found in configuration", bucketPublicName)
	}

	bucketCfg, ok := buckets[regionAlias]
	if !ok {
		log.Fatalf("[FATA] Region '%s' not found for bucket '%s'", regionAlias, bucketPublicName)
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
		log.Fatalf("[FATA] Failed to upload file: %v", err)
	}

	log.Printf("[INFO] Successfully uploaded %s to s3://%s/%s (Content-Type: %s)", expandedPath, bucketName, fullPath, contentType)
}
