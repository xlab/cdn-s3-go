FROM golang:1.25-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION_FLAGS
RUN CGO_ENABLED=0 go build -ldflags "${VERSION_FLAGS}" -o cdn-s3-go ./cmd/cdn-s3-go

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /build/cdn-s3-go .

EXPOSE 8080

ENTRYPOINT ["/app/cdn-s3-go"]
