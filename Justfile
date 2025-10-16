run *args: modules
    go run . {{ quote(args) }}

modules:
    go mod vendor

# run the tests
test: modules
    go test -v ./...

build: modules
    go build -o asimi .

dlv:
    go install github.com/go-delve/delve/cmd/dlv@latest

debug: dlv
    dlv --listen=:2345 --headless=true --api-version=2 --accept-multiclient exec ./asimi -- --config config/default.toml
