FROM --platform=$BUILDPLATFORM golang:1.27.2-bookworm@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61 AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY testdata/compat/config.toml testdata/compat/import.csv testdata/compat/import.json testdata/compat/query.json ./testdata/compat/
RUN go test ./... && go vet ./...
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X github.com/danielfrg/dinero/internal/cli.Version=${VERSION}" -o /out/dinero ./cmd/dinero

FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
COPY --from=build /out/dinero /dinero
COPY LICENSE.txt /licenses/LICENSE.txt
ARG VCS_REF
ARG VERSION=dev
LABEL org.opencontainers.image.source="https://github.com/danielfrg/dinero" \
      org.opencontainers.image.revision=$VCS_REF \
      org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.licenses="Apache-2.0"
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/dinero", "serve", "--host", "0.0.0.0", "--port", "8080"]
