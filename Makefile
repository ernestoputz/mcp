BINARY     := mcp-server
IMAGE_NAME := mcp-observability
IMAGE_TAG  := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
REGISTRY   := your-registry  # ← change this

.PHONY: build run test docker-build docker-build-amd64 docker-build-arm64 docker-build-multi docker-push k8s-apply k8s-secret lint tidy

## Build local binary
build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/$(BINARY) ./cmd/server

## Run locally (requires .env)
run: build
	@set -a; source .env; set +a; MCP_TRANSPORT=http ./bin/$(BINARY)

## Run tests
test:
	go test ./... -race -count=1

## Lint
lint:
	golangci-lint run ./...

## Tidy modules
tidy:
	go mod tidy

## Build Docker image for the host architecture
docker-build:
	docker build -t $(IMAGE_NAME):$(IMAGE_TAG) -t $(IMAGE_NAME):latest .

## Build Docker image for linux/amd64 (x86_64 servers, cloud)
docker-build-amd64:
	docker buildx build --platform linux/amd64 \
		-t $(IMAGE_NAME):$(IMAGE_TAG)-amd64 -t $(IMAGE_NAME):amd64 \
		--load .

## Build Docker image for linux/arm64 (Raspberry Pi 4/5 64-bit, Apple Silicon, AWS Graviton)
docker-build-arm64:
	docker buildx build --platform linux/arm64 \
		-t $(IMAGE_NAME):$(IMAGE_TAG)-arm64 -t $(IMAGE_NAME):arm64 \
		--load .

## Build a multi-arch manifest (amd64+arm64) and push to $(REGISTRY)
docker-build-multi:
	docker buildx build --platform linux/amd64,linux/arm64 \
		-t $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG) \
		-t $(REGISTRY)/$(IMAGE_NAME):latest \
		--push .

## Push to registry
docker-push: docker-build
	docker tag $(IMAGE_NAME):$(IMAGE_TAG) $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)
	docker tag $(IMAGE_NAME):latest $(REGISTRY)/$(IMAGE_NAME):latest
	docker push $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)
	docker push $(REGISTRY)/$(IMAGE_NAME):latest

## Create Kubernetes secret from .env file (run once)
k8s-secret:
	@if [ ! -f .env ]; then echo "ERROR: .env not found. Copy .env.example to .env and fill it in."; exit 1; fi
	@set -a; source .env; set +a; \
	kubectl create secret generic mcp-observability-secrets \
		--namespace mcp-observability \
		--from-literal=PROMETHEUS_URL="$$PROMETHEUS_URL" \
		--from-literal=PROMETHEUS_USERNAME="$$PROMETHEUS_USERNAME" \
		--from-literal=PROMETHEUS_PASSWORD="$$PROMETHEUS_PASSWORD" \
		--from-literal=PROMETHEUS_BEARER_TOKEN="$$PROMETHEUS_BEARER_TOKEN" \
		--from-literal=GRAFANA_URL="$$GRAFANA_URL" \
		--from-literal=GRAFANA_API_KEY="$$GRAFANA_API_KEY" \
		--from-literal=GRAFANA_USERNAME="$$GRAFANA_USERNAME" \
		--from-literal=GRAFANA_PASSWORD="$$GRAFANA_PASSWORD" \
		--from-literal=GRAFANA_ORG_ID="$$GRAFANA_ORG_ID" \
		--from-literal=MCP_AUTH_TOKEN="$$MCP_AUTH_TOKEN" \
		--dry-run=client -o yaml | kubectl apply -f -
	@echo "Secret applied to namespace mcp-observability"

## Apply all Kubernetes manifests
k8s-apply: k8s-secret
	kubectl apply -f k8s/deployment.yaml

## Show help
help:
	@grep -E '^##' Makefile | sed 's/## /  /'
