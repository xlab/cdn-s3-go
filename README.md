# cdn-s3-go

This is a service implementing CDN using S3 compatible buckets as backend. It's geo distributed, uses presigned URLs to avoid exposing buckets to public, and most importantly utilizes HTTP 302 redirects to return content without proxying the data.

Key benefits:

* No additional ingress/egress costs - users are pointed directly to S3 buckets.
* Geo distributed single endpoint, for cases when buckets themselves are not.
* Low latency for cached requests.

## Running

```bash
docker run -it --rm xlab/cdn-s3-go
```

## Building

Native build requires [Go 1.25](https://go.dev/dl/) installed.

```bash
make install
```

Docker image build:

```bash
make buildx
```

## Configuration

Program accepts list of bucket names as env variable `CDN_BUCKET_PUBLIC_NAMES` like `lfs,avatars`.

It also accepts list of region aliases as env variable `CDN_BUCKET_REGION_ALIASES` like `us,eu`.

For every bucket name in the list, it tries to load AWS S3 compatible credentials:

```bash
CDN_BUCKET_ENDPOINT_<public_bucket_name>_<region_alias>
CDN_BUCKET_REGION_<public_bucket_name>_<region_alias>
CDN_BUCKET_NAME_<public_bucket_name>_<region_alias>
CDN_BUCKET_PATH_PREFIX_<public_bucket_name>_<region_alias>
CDN_BUCKET_ACCESS_KEY_ID_<public_bucket_name>_<region_alias>
CDN_BUCKET_SECRET_ACCESS_KEY_<public_bucket_name>_<region_alias>
```

For example, if you have only one bucket `avatars` in region `us`, you can set the following environment variables:

```bash
CDN_BUCKET_PUBLIC_NAMES=avatars
CDN_BUCKET_REGION_ALIASES=us

CDN_BUCKET_ENDPOINT_avatars_us=https://s3.amazonaws.com
CDN_BUCKET_REGION_avatars_us=us-east-1
CDN_BUCKET_NAME_avatars_us=lemon-saddlebag-uces-n
CDN_BUCKET_PATH_PREFIX_avatars_us=
CDN_BUCKET_ACCESS_KEY_ID_avatars_us=AKIAEXAMPLE
CDN_BUCKET_SECRET_ACCESS_KEY_avatars_us=PRIVVVVV
```

See a fuller example in [.env.example](/.env.example) with multiple buckets in multiple regions.

## Principle of work

It starts a high-performant web server that listens for GET queries and routes public bucket names paths into corresponding S3 buckets.

Example routing: `GET /avatars/user1/a.jpg` gets the bucket `:public_bucket_name` from the first segment of URL, the rest is bucket-related path. It looks for that path in buckets `us` and `eu` in parallel. Then gets a pre-signed URL for that file and issues a HTTP 302 redirect.

Important variable `CDN_BUCKET_PATH_PREFIX_` controls whether files are not in the root of the bucket, but prefixed like `/public` - in this case `/avatars/user1/a.jpg` would mean "look for `/public/user1/a.jpg` in bucket `avatars`".

## Redis Caching (Optional)

To improve performance and reduce S3 API calls, you can enable Redis caching for presigned URLs:

```bash
CDN_URL_CACHE_REDIS=redis://localhost:6379/0
```

When configured:

* Presigned URLs are cached in Redis with key format `<bucket>:<path>`
* Cache TTL is set to match the Cache-Control header (30 days - 8 hours)
* Cache lookups happen before S3 HEAD requests
* If Redis is unavailable or not configured, the service continues without caching
* Items loaded from cache have a proper expiration date, so Cache-Control header is set correctly

Redis connection string format: `redis://[user:password@]host:port/database`

## License

Copyright (c) 2025 <xlab@upd.dev>

GNU GPL v3 see [LICENSE](LICENSE)
