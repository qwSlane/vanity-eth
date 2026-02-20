BINARY  := vanity-eth
VERSION ?= dev

.PHONY: build install clean build-all

build:
	go build -ldflags "-s -w" -o $(BINARY) .

install:
	go install .

clean:
	rm -f $(BINARY) $(BINARY)-*

build-all:
	GOOS=darwin  GOARCH=amd64  go build -ldflags "-s -w" -o $(BINARY)-darwin-amd64   .
	GOOS=darwin  GOARCH=arm64  go build -ldflags "-s -w" -o $(BINARY)-darwin-arm64   .
	GOOS=linux   GOARCH=amd64  go build -ldflags "-s -w" -o $(BINARY)-linux-amd64    .
	GOOS=linux   GOARCH=arm64  go build -ldflags "-s -w" -o $(BINARY)-linux-arm64    .
	GOOS=windows GOARCH=amd64  go build -ldflags "-s -w" -o $(BINARY)-windows-amd64.exe .
