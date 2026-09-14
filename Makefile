# Teamster is a standalone module; a parent go.work would pull in unrelated modules.
export GOWORK := off

COVERAGE_MIN ?= 75
TAILWIND_VERSION ?= v4.3.3
SQLC_VERSION ?= v1.31.1
POSTGRES_IMAGE ?= postgres:18-alpine
POSTGRES_DSN ?= postgres://teamster:teamster@127.0.0.1:15432/teamster?sslmode=disable
COVERAGE_OUT ?= coverage.out
BINARY       ?= bin/teamster
IMAGE        ?= teamster
CHART        ?= charts/teamster
CONTAINER_TOOL ?= $(shell command -v docker >/dev/null 2>&1 && echo docker || echo podman)
VERSION      ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: all build run test test-postgres db-up db-down coverage coverage-html fmt lint tidy hooks tools generate icons image image-run chart-lint clean

# The admin UI wears logo 1; logo 2 is the README header.
ICON_SOURCE := images/favicons/logo-teamster-1
SERVED_ICONS := favicon-16x16.png favicon-32x32.png favicon-48x48.png \
	apple-touch-icon.png icon-192-maskable.png icon-512-maskable.png

all: generate lint coverage build

# templ ships as a go tool dependency, so only Tailwind needs fetching. The
# generated output is committed, so this is needed only to change it.
tools: bin/tailwindcss bin/sqlc

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

# sqlc is a downloaded binary rather than a go.mod tool dependency: it depends
# on a cgo SQL parser, which a tool directive would drag into go.sum and make
# `go mod download` fetch on every image build.
bin/sqlc:
	@mkdir -p bin
	@case "$$(uname -s)-$$(uname -m)" in \
		Darwin-arm64) asset=darwin_arm64 ;; \
		Darwin-x86_64) asset=darwin_amd64 ;; \
		Linux-aarch64) asset=linux_arm64 ;; \
		Linux-x86_64) asset=linux_amd64 ;; \
		*) echo "no sqlc build for $$(uname -s)-$$(uname -m)" >&2; exit 1 ;; \
	esac; \
	version=$(SQLC_VERSION); \
	curl -fsSL "https://github.com/sqlc-dev/sqlc/releases/download/$$version/sqlc_$${version#v}_$$asset.tar.gz" \
		| tar -xzC bin sqlc
	@chmod +x bin/sqlc

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

# -race throughout: the store is reached from every request goroutine, so a
# data race here is a production bug rather than a test artefact.
test:
	go test -race ./...

# -coverpkg attributes coverage across packages, so code exercised through
# another package's tests counts; the generated output — templ's and sqlc's —
# is then filtered out, because generated code is not ours to test. sqlc is
# filtered by path, since it emits names as generic as db.go and models.go.
coverage:
	go test -race ./... -covermode=atomic -coverpkg=./... -coverprofile=$(COVERAGE_OUT).raw
	@grep -Ev '_templ\.go:|internal/store/(sqlitedb|pgdb)/|internal/store/pgqueries\.go:' $(COVERAGE_OUT).raw > $(COVERAGE_OUT)
	@go tool cover -func=$(COVERAGE_OUT) | tail -1
	@total=$$(go tool cover -func=$(COVERAGE_OUT) | awk '/^total:/ {print $$3}' | tr -d '%'); \
	awk -v total="$$total" -v min="$(COVERAGE_MIN)" 'BEGIN { \
		if (total+0 < min+0) { printf "coverage %.1f%% is below the required %s%%\n", total, min; exit 1 } \
		printf "coverage %.1f%% meets the required %s%%\n", total, min }'

# The Postgres backend's tests skip unless they are told where a server is.
# These start one with whatever container tool is here, so running them locally
# needs no more setup than the SQLite ones.
db-up:
	$(CONTAINER_TOOL) run -d --rm --name teamster-pg \
		-e POSTGRES_PASSWORD=teamster -e POSTGRES_USER=teamster -e POSTGRES_DB=teamster \
		-p 15432:5432 $(POSTGRES_IMAGE)
	@until $(CONTAINER_TOOL) exec teamster-pg pg_isready -U teamster >/dev/null 2>&1; do sleep 1; done
	@echo "postgres ready on 127.0.0.1:15432"

db-down:
	-$(CONTAINER_TOOL) stop teamster-pg

test-postgres:
	TEAMSTER_TEST_POSTGRES_DSN="$(POSTGRES_DSN)" go test -race ./internal/store/...

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
	@scripts/check-chart-render.sh $(CHART)

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
