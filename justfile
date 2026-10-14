default:
    just --list

# Go binary is the deployable CLI. Python remains available for existing consumers.
build:
    mkdir -p bin
    CGO_ENABLED=0 go build -o bin/dinero-go ./cmd/dinero

check:
    go test ./...
    go vet ./...
    go test -race ./...

compat: build
    uv run --offline python testdata/compat/check.py --go-executable bin/dinero-go

transactions: build
    bin/dinero-go transactions --yes

rules: build
    bin/dinero-go mkrules --commit --yes

dataset: build
    bin/dinero-go mkdataset

[working-directory: 'dashboards']
dashboards:
    uv run streamlit run balances.py

image:
    docker buildx build --platform linux/amd64 -t dinero:local .
