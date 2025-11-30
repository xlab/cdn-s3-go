package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
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

	for _, bucketName := range s.bucketPublicNames {
		bucketName = strings.TrimSpace(bucketName)
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
		opt, err := redis.ParseURL(redisURL)
		if err != nil {
			log.Printf("[WARN] Failed to parse CDN_URL_CACHE_REDIS: %v", err)
		} else {
			s.redisClient = redis.NewClient(opt)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			if err := s.redisClient.Ping(ctx).Err(); err != nil {
				log.Printf("[WARN] Failed to connect to Redis: %v", err)
				s.redisClient.Close()
				s.redisClient = nil
			} else {
				log.Printf("[INFO] Connected to Redis cache")
			}
		}
	}

	return s, nil
}

func (s *server) findObject(bucketName, objectPath string) (*bucketConfig, string, error) {
	buckets, ok := s.buckets[bucketName]
	if !ok {
		return nil, "", fmt.Errorf("bucket %s not found", bucketName)
	}

	for _, regionAlias := range s.regionAliases {
		regionAlias = strings.TrimSpace(regionAlias)
		bucketCfg, ok := buckets[regionAlias]
		if !ok {
			continue
		}

		fullPath := objectPath
		if bucketCfg.PathPrefix != "" {
			fullPath = strings.TrimPrefix(bucketCfg.PathPrefix, "/") + "/" + strings.TrimPrefix(objectPath, "/")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := bucketCfg.Client.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: aws.String(bucketCfg.BucketName),
			Key:    aws.String(fullPath),
		})
		cancel()

		if err == nil {
			return bucketCfg, fullPath, nil
		}
	}

	return nil, "", fmt.Errorf("object not found in any region")
}

func (s *server) getPresignedURL(bucketCfg *bucketConfig, objectPath string, expiry time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(bucketCfg.Client)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
	path = strings.TrimPrefix(path, "/")

	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		ctx.Error("Invalid path format", fasthttp.StatusBadRequest)
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
						// TODO: catch

						log.Printf("[WARN] Failed to parse Redis cache expiry for key %s: %v", cacheKey, scanErr)
						delCtx, delCancel := context.WithTimeout(context.Background(), defaultSlowRequestThreshold)
						s.redisClient.Del(delCtx, cacheKey)
						delCancel()
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
		ctx.Error("Object not found", fasthttp.StatusNotFound)
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
			// TODO: catch

			redisTTL := time.Second * time.Duration(cacheMaxAge)
			expTimestamp := time.Now().Add(redisTTL).Unix()

			redisCtx, cancel := context.WithTimeout(context.Background(), defaultSlowRequestThreshold)
			err := s.redisClient.HSet(redisCtx, cacheKey, "url", presignedURL, "exp", expTimestamp).Err()
			if err != nil {
				log.Printf("[WARN] Failed to cache URL in Redis: %v", err)
			} else {
				s.redisClient.Expire(redisCtx, cacheKey, redisTTL)
			}
			cancel()
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
