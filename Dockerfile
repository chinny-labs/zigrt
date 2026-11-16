# syntax=docker/dockerfile:1
# The release image (ADR 0012): built multi-arch with buildx. A static
# CGO_ENABLED=0 binary on distroless, /data owned by the nonroot user, the
# volume declared and Docker's HEALTHCHECK wired to `zigrt healthcheck`.
FROM --platform=$BUILDPLATFORM golang:1.27 AS build
ARG TARGETOS TARGETARCH
ARG VERSION=dev
ARG COMMIT=
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w -X github.com/chinny-labs/zigrt/internal/buildinfo.Version=${VERSION} -X github.com/chinny-labs/zigrt/internal/buildinfo.Commit=${COMMIT}" \
      -o /out/zigrt ./cmd/zigrt \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION=dev
LABEL org.opencontainers.image.title="zigrt" \
      org.opencontainers.image.description="Self-hosted mastery-learning maths for one household" \
      org.opencontainers.image.source="https://github.com/chinny-labs/zigrt" \
      org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.version="${VERSION}"
COPY --from=build /out/zigrt /usr/local/bin/zigrt
COPY --from=build --chown=65532:65532 /out/data /data
ENV ZIGRT_DATABASE_URL=sqlite:///data/zigrt.db
VOLUME /data
EXPOSE 8080
USER 65532:65532
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/usr/local/bin/zigrt", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/zigrt"]
CMD ["serve"]
