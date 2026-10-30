VERSION ?= 0.1.0
LDFLAGS := -s -w -X github.com/adalmao/adalmao/internal/cli.Version=$(VERSION)

.PHONY: build test cross clean

build:
	go build -ldflags "$(LDFLAGS)" -o dist/adalmao ./cmd/adalmao

test:
	go vet ./... && go test ./...

cross:
	GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/adalmao-linux-amd64 ./cmd/adalmao
	GOOS=linux   GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/adalmao-linux-arm64 ./cmd/adalmao
	GOOS=darwin  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/adalmao-darwin-amd64 ./cmd/adalmao
	GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/adalmao-darwin-arm64 ./cmd/adalmao
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/adalmao-windows-amd64.exe ./cmd/adalmao

clean:
	rm -rf dist
