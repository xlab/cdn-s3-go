# cdn-s3-go

## Description

This is a service implementing CDN using S3 compatible buckets as backend. It's geo distributed, uses presigned URLs to avoid exposing buckets to public and most importantly utilized HTTP 302 redirects to return content without proxyig the data.

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

For example, if you have two buckets `lfs` and `avatars`, you can set the following environment variables:

```bash
CDN_BUCKET_PUBLIC_NAMES=lfs,avatars
CDN_BUCKET_REGION_ALIASES=us

CDN_BUCKET_ENDPOINT_lfs_us=https://s3.amazonaws.com
CDN_BUCKET_REGION_lfs_us=us-east-1
CDN_BUCKET_NAME_lfs_us=lemon-saddlebag-uces-n
CDN_BUCKET_PATH_PREFIX_lfs_us=
CDN_BUCKET_ACCESS_KEY_ID_lfs_us=AKIAEXAMPLE
CDN_BUCKET_SECRET_ACCESS_KEY_lfs_us=PRIVVVVV
```

It starts a high-performant web server that listens for GET queries and routes public bucket names paths into corresponding S3 buckets.

It tries each region alias in order as specified, e.g. for `us,eu`, it will use S3 HEAD request to check if file exists in US bucket first, then EU bucket, before returning 404. First item found is returned.

This is made to support apps deployed in different regions and uploading to each bucket individually via private connections, those files can be then exposed via single CDN surface globally.

Example routing: `GET /avatars/user1/a.jpg` gets the bucket `:public_bucket_name` from the first segment of URL, the rest is bucket-related path. It looks for that path in bucket us, then eu, until found. Then gets a pre-signed URL for that file and issues a HTTP 302 redirect.

Important variable `CDN_BUCKET_PATH_PREFIX_` controls whether files are not in the root of the bucket, but prefixed like `/public` - in this case `/avatars/user1/a.jpg` means "look for /public/user1/a.jpg in bucket avatars".

## LICENSE

Copyright (c) 2025 <xlab@upd.dev>

GNU GPL v3 see [LICENSE](LICENSE)
