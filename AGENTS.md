# AGENTS.md

## Commands

- Build/install: `make install` or `go install ./cmd/cdn-s3-go`
- Run server: `cdn-s3-go start` (after building)
- Env variables template: `cat .env.example`

## Architecture

- Single binary CDN service using S3-compatible buckets as backend
- Main entry point: cmd/cdn-s3-go/main.go (commands: start, upload, version)
- Core server logic: cmd/cdn-s3-go/server.go (fasthttp server, S3 client, Redis caching)
- Version info: version/version.go (build metadata)
- Uses HTTP 302 redirects to presigned S3 URLs (no proxying)
- Optional Redis caching for presigned URLs with TTL
- Multi-region bucket support with auto routing

## Code Style

- Standard Go formatting (use `go fmt`)
- Imports: stdlib first, then external packages (AWS SDK, Redis, fasthttp), then internal (upd.dev/upd/cdn-s3-go/version)
- Error handling: return errors up the stack, log with golang standard slog
- Naming: camelCase for unexported, PascalCase for exported; descriptive names (bucketConfig, handleRequest)
- Environment variables: CDN_* prefix, uppercase with underscores
- Constants: descriptive names with units (defaultPresignedTTL =)
- Context timeouts: always use context.WithTimeout and defer cancel()
- Goroutines: always defer recover to catch panics
