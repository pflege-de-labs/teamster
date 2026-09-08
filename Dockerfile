# syntax=docker/dockerfile:1

FROM golang:1.24 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
	--mount=type=cache,target=/root/.cache/go-build \
	CGO_ENABLED=0 go build \
	-trimpath \
	-ldflags "-s -w -X main.version=${VERSION}" \
	-o /out/teamster ./cmd/server

# The final image has no shell, so the writable data directory is prepared here.
RUN install -d -m 0755 -o 65532 -g 65532 /out/data

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/teamster /usr/local/bin/teamster
COPY --from=build --chown=65532:65532 /out/data /data

# Config file locations follow XDG, so /etc/xdg is the mount point for one.
ENV TEAMSTER_DATABASE_PATH=/data/teamster.db

USER nonroot:nonroot
WORKDIR /data
EXPOSE 8080
VOLUME ["/data"]

ENTRYPOINT ["/usr/local/bin/teamster"]
CMD ["serve"]
