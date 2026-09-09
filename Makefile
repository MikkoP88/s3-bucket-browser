VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
# Wails needs its tags for a working GUI; without them the binary shows
# Wails' "will not build without the correct build tags" error instead of
# the app (https://wails.io/docs/guides/manual-builds).
GUI_TAGS := desktop,production
BINARY  := s3b

.PHONY: build build-all test lint fmt run clean

build:
	go build -tags $(GUI_TAGS) -ldflags "$(LDFLAGS)" -o bin/$(BINARY)$(shell go env GOEXE) ./cmd/s3b

build-all:
	GOOS=windows GOARCH=amd64 go build -tags $(GUI_TAGS) -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-windows-amd64.exe ./cmd/s3b
	GOOS=windows GOARCH=arm64 go build -tags $(GUI_TAGS) -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-windows-arm64.exe ./cmd/s3b
	GOOS=linux   GOARCH=amd64 go build -tags $(GUI_TAGS) -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-amd64 ./cmd/s3b
	GOOS=linux   GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -tags s3b_headless -o dist/$(BINARY)-linux-arm64 ./cmd/s3b
	GOOS=darwin  GOARCH=amd64 go build -tags $(GUI_TAGS) -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-amd64 ./cmd/s3b
	GOOS=darwin  GOARCH=arm64 go build -tags $(GUI_TAGS) -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-arm64 ./cmd/s3b

test:
	go test -race ./...

lint:
	golangci-lint run

fmt:
	gofmt -l -w .

run: build
	./bin/$(BINARY)

clean:
	rm -rf bin dist
