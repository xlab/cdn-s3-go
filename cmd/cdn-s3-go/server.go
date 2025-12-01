package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/redis/go-redis/v9"
	"github.com/valyala/fasthttp"
)

type bucketConfig struct {
	Endpoint        string
	Region          string
	BucketName      string
	PathPrefix      string
	AccessKeyID     string
	SecretAccessKey string
	Client          *s3.Client
}

type server struct {
	bucketPublicNames []string
	regionAliases     []string
	buckets           map[string]map[string]*bucketConfig
	redisClient       *redis.Client
}

func newServer() (*server, error) {
	publicNames := os.Getenv("CDN_BUCKET_PUBLIC_NAMES")
	if publicNames == "" {
		return nil, fmt.Errorf("CDN_BUCKET_PUBLIC_NAMES is required")
	}

	regionAliases := os.Getenv("CDN_BUCKET_REGION_ALIASES")
	if regionAliases == "" {
		return nil, fmt.Errorf("CDN_BUCKET_REGION_ALIASES is required")
	}

	s := &server{
		bucketPublicNames: strings.Split(publicNames, ","),
		regionAliases:     strings.Split(regionAliases, ","),
		buckets:           make(map[string]map[string]*bucketConfig),
	}

	for i, regionAlias := range s.regionAliases {
		s.regionAliases[i] = strings.TrimSpace(regionAlias)
	}

	for i, bucketPublicName := range s.bucketPublicNames {
		s.bucketPublicNames[i] = strings.TrimSpace(bucketPublicName)
	}

	for _, bucketName := range s.bucketPublicNames {
		s.buckets[bucketName] = make(map[string]*bucketConfig)

		for _, regionAlias := range s.regionAliases {
			regionAlias = strings.TrimSpace(regionAlias)

			endpoint := os.Getenv(fmt.Sprintf("CDN_BUCKET_ENDPOINT_%s_%s", bucketName, regionAlias))
			region := os.Getenv(fmt.Sprintf("CDN_BUCKET_REGION_%s_%s", bucketName, regionAlias))
			actualBucketName := os.Getenv(fmt.Sprintf("CDN_BUCKET_NAME_%s_%s", bucketName, regionAlias))
			pathPrefix := os.Getenv(fmt.Sprintf("CDN_BUCKET_PATH_PREFIX_%s_%s", bucketName, regionAlias))
			accessKeyID := os.Getenv(fmt.Sprintf("CDN_BUCKET_ACCESS_KEY_ID_%s_%s", bucketName, regionAlias))
			secretAccessKey := os.Getenv(fmt.Sprintf("CDN_BUCKET_SECRET_ACCESS_KEY_%s_%s", bucketName, regionAlias))

			if endpoint == "" || region == "" || actualBucketName == "" || accessKeyID == "" || secretAccessKey == "" {
				log.Printf("[WARN] Skipping bucket %s region %s due to missing configuration", bucketName, regionAlias)
				continue
			}

			bucketCfg := &bucketConfig{
				Endpoint:        endpoint,
				Region:          region,
				BucketName:      actualBucketName,
				PathPrefix:      pathPrefix,
				AccessKeyID:     accessKeyID,
				SecretAccessKey: secretAccessKey,
			}

			customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{
					URL:               endpoint,
					SigningRegion:     bucketCfg.Region,
					HostnameImmutable: true,
				}, nil
			})

			awsCfg, err := config.LoadDefaultConfig(context.Background(),
				config.WithRegion(bucketCfg.Region),
				config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
					accessKeyID,
					secretAccessKey,
					"",
				)),
				config.WithEndpointResolverWithOptions(customResolver),
			)
			if err != nil {
				return nil, fmt.Errorf("failed to create AWS config for %s/%s: %w", bucketName, regionAlias, err)
			}

			bucketCfg.Client = s3.NewFromConfig(awsCfg)
			s.buckets[bucketName][regionAlias] = bucketCfg

			log.Printf("[INFO] Configured bucket: %s, region: %s, actual bucket name: %s", bucketName, regionAlias, actualBucketName)
		}
	}

	redisURL := os.Getenv("CDN_URL_CACHE_REDIS")
	if redisURL != "" {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[WARN] Redis initialization panicked: %v", r)
					s.redisClient = nil
				}
			}()

			opt, err := redis.ParseURL(redisURL)
			if err != nil {
				log.Printf("[WARN] Failed to parse CDN_URL_CACHE_REDIS: %v", err)
			} else {
				s.redisClient = redis.NewClient(opt)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				if err := s.redisClient.Ping(ctx).Err(); err != nil {
					log.Printf("[WARN] Failed to connect to Redis: %v", err)
					s.redisClient.Close()
					s.redisClient = nil
				} else {
					log.Printf("[INFO] Connected to Redis cache")
				}
			}
		}()
	}

	return s, nil
}

type findResult struct {
	config   *bucketConfig
	fullPath string
}

func (s *server) findObject(bucketName, objectPath string) (*bucketConfig, string, error) {
	buckets, ok := s.buckets[bucketName]
	if !ok {
		return nil, "", fmt.Errorf("bucket %s not found", bucketName)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resultChan := make(chan findResult, len(s.regionAliases))
	wg := new(sync.WaitGroup)
	wg.Add(len(s.regionAliases))

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	for _, regionAlias := range s.regionAliases {
		bucketCfg, ok := buckets[regionAlias]
		if !ok {
			wg.Done()
			continue
		}

		fullPath := objectPath
		if bucketCfg.PathPrefix != "" {
			fullPath = strings.TrimPrefix(bucketCfg.PathPrefix, "/") + "/" + strings.TrimPrefix(objectPath, "/")
		}

		go func(regionAlias string, cfg *bucketConfig, path string) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[WARN] findObject goroutine panicked on bucket %s (region %s): %v", cfg.BucketName, regionAlias, r)
				}
			}()

			headCtx, headCancel := context.WithTimeout(ctx, defaultSlowRequestThreshold)
			defer headCancel()

			_, err := cfg.Client.HeadObject(headCtx, &s3.HeadObjectInput{
				Bucket: aws.String(cfg.BucketName),
				Key:    aws.String(path),
			})

			if err == nil {
				resultChan <- findResult{config: cfg, fullPath: path}
				return
			}
		}(regionAlias, bucketCfg, fullPath)
	}

	// blocks until 1 found result, ErrNotFound if no results or a timeout
	for {
		select {
		case result, ok := <-resultChan:
			if !ok {
				// channels is closed - all parallel lookups are done
				return nil, "", ErrNotFound
			}
			// return immediately (cancels ctx and all ongoing requests, discards any channel results)
			go drainChain(resultChan)
			return result.config, result.fullPath, nil

		case <-time.After(defaultSlowRequestThreshold):
			go drainChain(resultChan)
			return nil, "", ErrNotFound
		}
	}
}

func drainChain(c <-chan findResult) {
	for range c {
	}
}

var ErrNotFound = errors.New("not found")

func (s *server) getPresignedURL(bucketCfg *bucketConfig, objectPath string, expiry time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(bucketCfg.Client)

	ctx, cancel := context.WithTimeout(context.Background(), defaultSlowRequestThreshold)
	defer cancel()

	req, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketCfg.BucketName),
		Key:    aws.String(objectPath),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = expiry
	})

	if err != nil {
		return "", err
	}

	return req.URL, nil
}

// defaultPresignedTTL controls pre-signed URL validity, also TTL for Cache-Control and Redis
const defaultPresignedTTL = 30 * 24 * time.Hour
const defaultSlowRequestThreshold = 5 * time.Second
const defaultFastRequestThreshold = 500 * time.Millisecond

func (s *server) handleRequest(ctx *fasthttp.RequestCtx) {
	path := string(ctx.Path())

	if path == "/health" {
		ctx.SetStatusCode(200)
		ctx.SetBodyString("OK")
		return
	}

	path = strings.TrimPrefix(path, "/")

	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		ctx.Error("Not found", fasthttp.StatusNotFound)
		return
	}

	bucketName := parts[0]
	objectPath := parts[1]

	var cacheKey string
	if s.redisClient != nil {
		cacheKey = fmt.Sprintf("%s:%s", bucketName, objectPath)

		redisCtx, cancel := context.WithTimeout(context.Background(), defaultFastRequestThreshold)
		result, err := s.redisClient.HMGet(redisCtx, cacheKey, "url", "exp").Result()
		cancel()

		if err == nil && len(result) == 2 && result[0] != nil && result[1] != nil {
			cachedURL, urlOk := result[0].(string)
			expStr, expOk := result[1].(string)

			if urlOk && expOk && cachedURL != "" {
				var expTimestamp int64
				_, scanErr := fmt.Sscanf(expStr, "%d", &expTimestamp)
				if scanErr != nil {
					go func() {
						defer func() {
							if r := recover(); r != nil {
								log.Printf("[WARN] Redis cache deletion panicked for key %s: %v", cacheKey, r)
							}
						}()

						log.Printf("[WARN] Failed to parse Redis cache expiry for key %s: %v", cacheKey, scanErr)
						delCtx, delCancel := context.WithTimeout(context.Background(), defaultSlowRequestThreshold)
						defer delCancel()

						_ = s.redisClient.Del(delCtx, cacheKey)
					}()
				} else {
					timeLeft := expTimestamp - time.Now().Unix()
					if timeLeft > 0 {
						ctx.Response.Header.Set("Cache-Control", fmt.Sprintf("max-age=%d", timeLeft))
						ctx.Redirect(cachedURL, fasthttp.StatusFound)
						return
					}
				}
			}
		}
	}

	bucketCfg, fullPath, err := s.findObject(bucketName, objectPath)
	if err != nil {
		ctx.Error("Not found", fasthttp.StatusNotFound)
		return
	}

	presignedURL, err := s.getPresignedURL(bucketCfg, fullPath, defaultPresignedTTL)
	if err != nil {
		log.Printf("[WARN] Error generating presigned URL: %v", err)
		ctx.Error("Internal server error", fasthttp.StatusInternalServerError)
		return
	}

	cacheMaxAge := int((defaultPresignedTTL - 8*time.Hour).Seconds())
	if s.redisClient != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[WARN] Redis cache storage panicked for key %s: %v", cacheKey, r)
				}
			}()

			redisTTL := time.Second * time.Duration(cacheMaxAge)
			expTimestamp := time.Now().Add(redisTTL).Unix()

			redisCtx, cancel := context.WithTimeout(context.Background(), defaultSlowRequestThreshold)
			defer cancel()

			err := s.redisClient.HSet(redisCtx, cacheKey, "url", presignedURL, "exp", expTimestamp).Err()
			if err != nil {
				log.Printf("[WARN] Failed to cache URL in Redis: %v", err)
			} else {
				_ = s.redisClient.Expire(redisCtx, cacheKey, redisTTL)
			}
		}()
	}

	ctx.Response.Header.Set("Cache-Control", fmt.Sprintf("max-age=%d", cacheMaxAge))
	ctx.Redirect(presignedURL, fasthttp.StatusFound)
}

// readEnv is a special utility that reads `.env` file into actual environment variables
// of the current app, similar to `dotenv` Node package.
func readEnv() {
	if envdata, _ := os.ReadFile(".env"); len(envdata) > 0 {
		s := bufio.NewScanner(bytes.NewReader(envdata))
		for s.Scan() {
			txt := s.Text()
			valIdx := strings.IndexByte(txt, '=')
			if valIdx < 0 {
				continue
			}

			strValue := strings.Trim(txt[valIdx+1:], `"`)
			_ = os.Setenv(txt[:valIdx], strValue)
		}
	}
}
