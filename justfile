default: run

build:
    go build -o r9s ./cmd/r9s

run *args: build
    ./r9s {{args}}

test:
    go vet ./...
    go test ./...

fmt:
    gofmt -w .

install:
    go install ./cmd/r9s

uninstall:
    rm -f "$(go env GOBIN)/r9s" "$(go env GOPATH)/bin/r9s"

clean:
    rm -f r9s
