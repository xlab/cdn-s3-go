FROM golang:1.25-alpine AS builder

WORKDIR /build

COPY go.mod go.sum* ./
RUN go mod download

COPY *.go ./

RUN CGO_ENABLED=0 go build -o cdn .

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /build/cdn .

EXPOSE 8080

CMD ["./cdn"]
