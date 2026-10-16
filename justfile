default:
    just --list

install:
    go mod download

api:
    go run ./cmd/dinero serve

# Build the Dinero CLI and server.
build:
    mkdir -p bin
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/dinero ./cmd/dinero

check:
    go test -race ./...
    go vet ./...
    bash scripts/install_test.sh
    bash scripts/package_test.sh
    bash scripts/publish-main_test.sh

transactions: build
    bin/dinero transactions --yes

rules: build
    bin/dinero mkrules --commit --yes

dataset: build
    bin/dinero mkdataset

image:
    docker build -t dinero:local .

image-check: image
    bash scripts/container-smoke.sh dinero:local
