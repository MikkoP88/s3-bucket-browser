VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(patsubst v%,%,$(VERSION))
TARGET_OS := $(if $(GOOS),$(GOOS),$(shell go env GOOS))
# Wails v3: the GUI is the default build — `production` strips devtools
# (https://wails.io/docs/guides/manual-builds).
# Linux uses GTK3/WebKitGTK 4.1; macOS uses the system WebKit.
comma := ,
GUI_TAGS := production$(if $(filter linux,$(TARGET_OS)),$(comma)gtk3)
BINARY  := s3b

# LDFLAGS_GUI: host GUI build — on Windows link with -H windowsgui: without
# it Windows allocates a console for the process and the app flashes a black
# console window on every launch (detachConsole frees it, but only after it
# appeared once). The CLI still works: internal/cli reattaches the parent
# terminal. Same flag as release.yml; invalid on ELF/Mach-O, so
# cross-compiled targets (build-all) set it per-line.
LDFLAGS_GUI := $(LDFLAGS) $(if $(filter windows,$(TARGET_OS)),-H windowsgui)

ifeq ($(TARGET_OS),darwin)
build test run lint: export CGO_ENABLED := 1
build test run lint: export MACOSX_DEPLOYMENT_TARGET := 13.0
build test run lint: export CGO_CFLAGS := $(CGO_CFLAGS) -mmacosx-version-min=13.0
build test run lint: export CGO_LDFLAGS := $(CGO_LDFLAGS) -mmacosx-version-min=13.0 -framework UniformTypeIdentifiers
endif

.PHONY: build mac mac-universal build-all test lint fmt run clean

build:
	go build -tags $(GUI_TAGS) -ldflags "$(LDFLAGS_GUI)" -o bin/$(BINARY)$(if $(filter windows,$(TARGET_OS)),.exe) ./cmd/s3b

# Build and ad-hoc sign a Finder-launchable application on a Mac.
mac:
	bash scripts/build-macos.sh

mac-universal:
	bash scripts/build-macos.sh universal

build-all:
	GOOS=windows GOARCH=amd64 go build -tags production -ldflags "$(LDFLAGS) -H windowsgui" -o dist/$(BINARY)-windows-amd64.exe ./cmd/s3b
	GOOS=windows GOARCH=arm64 go build -tags production -ldflags "$(LDFLAGS) -H windowsgui" -o dist/$(BINARY)-windows-arm64.exe ./cmd/s3b
	# gtk3: Wails v3 defaults to GTK4/webkitgtk-6.0; Ubuntu 24.04 ships 4.1
	GOOS=linux   GOARCH=amd64 go build -tags production,gtk3 -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-amd64 ./cmd/s3b
	GOOS=linux   GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -tags s3b_headless -o dist/$(BINARY)-linux-arm64 ./cmd/s3b
	# darwin: use the same minimum as the local .app build (Go 1.27: 13.0).
	GOOS=darwin  GOARCH=amd64 CGO_ENABLED=1 MACOSX_DEPLOYMENT_TARGET=13.0 \
	  CGO_CFLAGS="-mmacosx-version-min=13.0" \
	  CGO_LDFLAGS="-mmacosx-version-min=13.0 -framework UniformTypeIdentifiers" \
	  go build -tags production -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-amd64 ./cmd/s3b
	GOOS=darwin  GOARCH=arm64 CGO_ENABLED=1 MACOSX_DEPLOYMENT_TARGET=13.0 \
	  CGO_CFLAGS="-mmacosx-version-min=13.0" \
	  CGO_LDFLAGS="-mmacosx-version-min=13.0 -framework UniformTypeIdentifiers" \
	  go build -tags production -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-arm64 ./cmd/s3b

test:
	go test -race -tags $(GUI_TAGS) ./...

lint:
	golangci-lint run

fmt:
	gofmt -l -w .

run: build
	./bin/$(BINARY)

clean:
	rm -rf bin dist
