modules:
    go mod vendor

# run the tests
test: modules
    go test -v ./...

build: modules
    go build -o asimi .
