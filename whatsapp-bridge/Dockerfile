# syntax=docker/dockerfile:1
# wa-gateway (PLANO.md §9): a static Go binary (pure-Go SQLite, no CGO) on distroless, as a non-root user.
# The build stage runs on the builder's own platform and cross-compiles, so an ARM64 image
# builds on an x86 machine without emulation:  docker buildx build --platform linux/arm64 .

FROM --platform=$BUILDPLATFORM golang:1.26 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
        go build -trimpath -ldflags='-s -w' -o /out/wa-gateway ./cmd/wa-gateway \
    && mkdir /out/data /out/media

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/wa-gateway /wa-gateway
# Owned by the non-root user, so new named volumes mounted here start out writable.
COPY --from=build --chown=65532:65532 /out/data /data
COPY --from=build --chown=65532:65532 /out/media /media
ENV STORE_DIR=/data MEDIA_DIR=/media NO_COLOR=1
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/wa-gateway"]
CMD ["serve"]
