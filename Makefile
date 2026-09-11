# Teamster is a standalone module; a parent go.work would pull in unrelated modules.
export GOWORK := off

COVERAGE_MIN ?= 75
TAILWIND_VERSION ?= v4.3.3
COVERAGE_OUT ?= coverage.out
BINARY       ?= bin/teamster
IMAGE        ?= teamster
CHART        ?= charts/teamster
CONTAINER_TOOL ?= $(shell command -v docker >/dev/null 2>&1 && echo docker || echo podman)
VERSION      ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: all build run test coverage coverage-html fmt lint tidy hooks tools generate icons image image-run chart-lint clean

# The admin UI wears logo 1; logo 2 is the README header.
ICON_SOURCE := images/favicons/logo-teamster-1
SERVED_ICONS := favicon-16x16.png favicon-32x32.png favicon-48x48.png \
	apple-touch-icon.png icon-192-maskable.png icon-512-maskable.png

all: generate lint coverage build

# templ ships as a go tool dependency, so only Tailwind needs fetching. The
# generated output is committed, so this is needed only to change it.
tools: bin/tailwindcss

bin/tailwindcss:
	@mkdir -p bin
	@case "$$(uname -s)-$$(uname -m)" in \
		Darwin-arm64) asset=tailwindcss-macos-arm64 ;; \
		Darwin-x86_64) asset=tailwindcss-macos-x64 ;; \
		Linux-aarch64) asset=tailwindcss-linux-arm64 ;; \
		Linux-x86_64) asset=tailwindcss-linux-x64 ;; \
		*) echo "no tailwindcss build for $$(uname -s)-$$(uname -m)" >&2; exit 1 ;; \
	esac; \
	curl -fsSL -o bin/tailwindcss \
		"https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/$$asset"
	@chmod +x bin/tailwindcss

generate: tools
	go generate ./...

# The icons the binary serves are copies of the generated set under images/.
# Regenerating the set with scripts/make-favicons.sh does not update them, so
# this does — and icons_test.go fails when the two drift apart.
icons:
	@for icon in $(SERVED_ICONS); do cp $(ICON_SOURCE)/$$icon internal/httpserver/web/icons/$$icon; done
	cp $(ICON_SOURCE)/favicon.ico internal/httpserver/web/favicon.ico

build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY) ./cmd/teamster

run:
	go run ./cmd/teamster

test:
	go test ./...

# -coverpkg attributes coverage across packages, so code exercised through
# another package's tests counts; the templ output is then filtered out,
# because generated code is not ours to test.
coverage:
	go test ./... -covermode=atomic -coverpkg=./... -coverprofile=$(COVERAGE_OUT).raw
	@grep -v '_templ\.go:' $(COVERAGE_OUT).raw > $(COVERAGE_OUT)
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

# Mirrors the chart job in CI: lint, then render every deployment shape.
chart-lint:
	helm lint $(CHART) --values $(CHART)/ci/statefulset-values.yaml
	@for values in $(CHART)/ci/*-values.yaml; do \
		echo "rendering $$values"; \
		helm template teamster $(CHART) --values "$$values" > /dev/null; \
	done

image:
	$(CONTAINER_TOOL) build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

# Mounts ./config.yaml as the system-wide XDG config the container looks for.
image-run: image
	$(CONTAINER_TOOL) run --rm -p 8080:8080 \
		-v $(CURDIR)/config.yaml:/etc/xdg/teamster/config.yaml:ro \
		-v teamster-data:/data \
		$(IMAGE):$(VERSION)

clean:
	rm -rf bin $(COVERAGE_OUT) $(COVERAGE_OUT).raw coverage.html
