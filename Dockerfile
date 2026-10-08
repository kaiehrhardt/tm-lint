# syntax=docker/dockerfile:1

# The build stage runs on the build host's platform and cross-compiles for
# the target, so multi-arch images build without emulation.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/tm-lint .

# Alpine rather than scratch/distroless: GitLab CI and similar runners need a
# shell in the job image.
FROM alpine:3.24
COPY --from=build /out/tm-lint /usr/local/bin/tm-lint
USER 65534:65534
WORKDIR /work
ENTRYPOINT ["tm-lint"]
