.PHONY: build run run-dry test clean docker-build docker-run

APP_NAME := asutp-collector
CONFIG := config/config.local.yaml

# Build
build:
	go build -o $(APP_NAME) ./cmd/collector

# Run with real sender
run: build
	./$(APP_NAME) --config $(CONFIG)

# Run in dry-run mode (log instead of send)
run-dry: build
	./$(APP_NAME) --config $(CONFIG) --dry-run

# Run directly without building
dev:
	go run ./cmd/collector --config $(CONFIG) --dry-run

# Test
test:
	go test -v ./...

# Clean
clean:
	rm -f $(APP_NAME)
	rm -f buffer.db

# Docker build
docker-build:
	docker build -t $(APP_NAME):latest .

# Docker run
docker-run:
	docker run --rm -p 8080:8080 \
		-v $(PWD)/config:/app/config:ro \
		-e CONFIG_PATH=/app/config/config.local.yaml \
		$(APP_NAME):latest --dry-run

# Lint
lint:
	golangci-lint run ./...

# Tidy
tidy:
	go mod tidy
