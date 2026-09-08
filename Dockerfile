# syntax=docker/dockerfile:1@sha256:ecfaec9ed6d810b56388c508f4121597bfbba70d41a6dfeee4d8cad5f295fc32

# Building on the native platform and cross-compiling avoids QEMU emulation.
FROM --platform=$BUILDPLATFORM golang:1.27.1@sha256:512690a5660563b57d37ecc31129e7f136e831db2aed24a1dbeb8ad7380dc0fa AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
	--mount=type=cache,target=/root/.cache/go-build,id=go-build-$TARGETOS-$TARGETARCH \
	CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
	-trimpath \
	-ldflags "-s -w -X main.version=${VERSION}" \
	-o /out/teamster ./cmd/teamster

# The final image has no shell, so the writable data directory is prepared here.
RUN install -d -m 0755 -o 65532 -g 65532 /out/data

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

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
