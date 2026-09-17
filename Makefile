VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
# Wails v3: the GUI is the default build — `production` strips devtools
# (https://wails.io/docs/guides/manual-builds).
GUI_TAGS := production
BINARY  := s3b

# LDFLAGS_GUI: host GUI build — on Windows link with -H windowsgui: without
# it Windows allocates a console for the process and the app flashes a black
# console window on every launch (detachConsole frees it, but only after it
# appeared once). The CLI still works: internal/cli reattaches the parent
# terminal. Same flag as release.yml; invalid on ELF/Mach-O, so
# cross-compiled targets (build-all) set it per-line.
LDFLAGS_GUI := $(LDFLAGS) $(if $(filter-out windows,$(GOOS)),,-H windowsgui)

.PHONY: build build-all test lint fmt run clean

build:
	go build -tags $(GUI_TAGS) -ldflags "$(LDFLAGS_GUI)" -o bin/$(BINARY)$(shell go env GOEXE) ./cmd/s3b

build-all:
	GOOS=windows GOARCH=amd64 go build -tags $(GUI_TAGS) -ldflags "$(LDFLAGS) -H windowsgui" -o dist/$(BINARY)-windows-amd64.exe ./cmd/s3b
	GOOS=windows GOARCH=arm64 go build -tags $(GUI_TAGS) -ldflags "$(LDFLAGS) -H windowsgui" -o dist/$(BINARY)-windows-arm64.exe ./cmd/s3b
	# gtk3: Wails v3 defaults to GTK4/webkitgtk-6.0; Ubuntu 24.04 ships 4.1
	GOOS=linux   GOARCH=amd64 go build -tags $(GUI_TAGS),gtk3 -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-amd64 ./cmd/s3b
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
