BINARY  := vanity-eth
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X vanity-eth/cmd.version=$(VERSION)

.PHONY: build install clean build-all tag

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

install:
	go install -ldflags "$(LDFLAGS)" .

clean:
	rm -f $(BINARY) $(BINARY)-*

build-all:
	GOOS=darwin  GOARCH=amd64  go build -ldflags "$(LDFLAGS)" -o $(BINARY)-darwin-amd64   .
	GOOS=darwin  GOARCH=arm64  go build -ldflags "$(LDFLAGS)" -o $(BINARY)-darwin-arm64   .
	GOOS=linux   GOARCH=amd64  go build -ldflags "$(LDFLAGS)" -o $(BINARY)-linux-amd64    .
	GOOS=linux   GOARCH=arm64  go build -ldflags "$(LDFLAGS)" -o $(BINARY)-linux-arm64    .
	GOOS=windows GOARCH=amd64  go build -ldflags "$(LDFLAGS)" -o $(BINARY)-windows-amd64.exe .

# Create and push a release tag: make tag VERSION=v1.0.0
tag:
	@[ "$(VERSION)" != "dev" ] || (echo "usage: make tag VERSION=v1.2.3" && exit 1)
	git tag $(VERSION)
	git push origin $(VERSION)
