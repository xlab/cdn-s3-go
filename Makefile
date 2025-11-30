APP_VERSION = $(shell git describe --abbrev=0 --tags)
GIT_COMMIT = $(shell git rev-parse --short HEAD)
BUILD_DATE = $(shell date -u "+%Y-%m-%d")
VERSION_PKG = upd.dev/upd/cdn-s3-go/version
DOCKERHUB_IMAGE := xlab/cdn-s3-go
VERSION_FLAGS="-X $(VERSION_PKG).AppVersion=$(APP_VERSION) -X $(VERSION_PKG).GitCommit=$(GIT_COMMIT) -X $(VERSION_PKG).BuildDate=$(BUILD_DATE)"

# Get the tag from environment variable or use GIT_COMMIT
TAG ?= $(GIT_COMMIT)

install:
	go install \
		-ldflags $(VERSION_FLAGS) \
		./cmd/cdn-s3-go

# Build image locally for current platform using Docker Buildx
buildx:
	@echo "Building image for $(DOCKERHUB_IMAGE):$(TAG)"
	docker buildx build \
		--platform linux/amd64 \
		--build-arg VERSION_FLAGS=$(VERSION_FLAGS) \
		-t $(DOCKERHUB_IMAGE):$(TAG) \
		-t $(DOCKERHUB_IMAGE):latest \
		-f Dockerfile \
		--load \
		.

# Build and push multi-arch image to DockerHub
buildx-push:
	@echo "Building and pushing multi-arch image for $(DOCKERHUB_IMAGE):$(TAG)"
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--build-arg VERSION_FLAGS=$(VERSION_FLAGS) \
		-t $(DOCKERHUB_IMAGE):$(TAG) \
		-f Dockerfile \
		--push \
		.

# Build and push multi-arch image with latest tag
buildx-push-latest:
	@echo "Building and pushing multi-arch image with latest tag"
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--build-arg VERSION_FLAGS=$(VERSION_FLAGS) \
		-t $(DOCKERHUB_IMAGE):$(TAG) \
		-t $(DOCKERHUB_IMAGE):latest \
		-f Dockerfile \
		--push \
		.

# Create buildx builder if it doesn't exist
buildx-setup:
	docker buildx create --name go-builder --use --bootstrap || docker buildx use go-builder

# Remove buildx builder
buildx-clean:
	docker buildx rm go-builder || true

.PHONY: install buildx buildx-push buildx-push-latest buildx-setup buildx-clean
