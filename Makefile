# Teamster is a standalone module; a parent go.work would pull in unrelated modules.
export GOWORK := off

COVERAGE_MIN ?= 75
COVERAGE_OUT ?= coverage.out
BINARY       ?= bin/teamster
IMAGE        ?= teamster
CONTAINER_TOOL ?= $(shell command -v docker >/dev/null 2>&1 && echo docker || echo podman)
VERSION      ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: all build run test coverage coverage-html fmt lint tidy hooks image image-run clean

all: lint coverage build

build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY) ./cmd/teamster

run:
	go run ./cmd/teamster

test:
	go test ./...

coverage:
	go test ./... -covermode=atomic -coverprofile=$(COVERAGE_OUT)
	@go tool cover -func=$(COVERAGE_OUT) | tail -1
	@total=$$(go tool cover -func=$(COVERAGE_OUT) | awk '/^total:/ {print $$3}' | tr -d '%'); \
	awk -v total="$$total" -v min="$(COVERAGE_MIN)" 'BEGIN { \
		if (total+0 < min+0) { printf "coverage %.1f%% is below the required %s%%\n", total, min; exit 1 } \
		printf "coverage %.1f%% meets the required %s%%\n", total, min }'

coverage-html: coverage
	go tool cover -html=$(COVERAGE_OUT) -o coverage.html

fmt:
	go fmt ./...

lint:
	golangci-lint run

tidy:
	go mod tidy

# Installs both the pre-commit and the commit-msg hook.
hooks:
	pre-commit install --install-hooks

image:
	$(CONTAINER_TOOL) build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

# Mounts ./config.yaml as the system-wide XDG config the container looks for.
image-run: image
	$(CONTAINER_TOOL) run --rm -p 8080:8080 \
		-v $(CURDIR)/config.yaml:/etc/xdg/teamster/config.yaml:ro \
		-v teamster-data:/data \
		$(IMAGE):$(VERSION)

clean:
	rm -rf bin $(COVERAGE_OUT) coverage.html
