default:
    just --list

# Build the Dinero CLI and server.
build:
    mkdir -p bin
    CGO_ENABLED=0 go build -o bin/dinero ./cmd/dinero

check:
    go test ./...
    go vet ./...
    go test -race ./...

transactions: build
    bin/dinero transactions --yes

rules: build
    bin/dinero mkrules --commit --yes

dataset: build
    bin/dinero mkdataset

image:
    docker buildx build --platform linux/amd64 -t dinero:local .
