# Runs the tests, then cross-compiles the three self-contained binaries.
# Usage: make            (test + all binaries)
#        make test | make clean
GO      ?= go
LDFLAGS := -s -w
BUILD   := CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)"
BINS    := racing.exe racing-linux-amd64 racing-linux-arm64

.PHONY: all test build clean
.DEFAULT_GOAL := all

all: test build

test:
	$(GO) test ./...

build: test
	GOOS=windows GOARCH=amd64 $(BUILD) -o racing.exe .
	GOOS=linux GOARCH=amd64 $(BUILD) -o racing-linux-amd64 .
	GOOS=linux GOARCH=arm64 $(BUILD) -o racing-linux-arm64 .

clean:
	rm -f $(BINS)
