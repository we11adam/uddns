NAME=uddns
ENTRIES=main.go
BINDIR=bin
GIT_REV=$$(git rev-parse HEAD)
BUILD_TIME=$$(date -Iseconds)
DARWIN_AMD64=darwin-amd64
DARWIN_ARM64=darwin-arm64
LINUX_AMD64=linux-amd64


BUILD_OPTS=-trimpath
LDFLAGS+=-s -w

BUILD_OPTS+=-ldflags="${LDFLAGS}"

.PHONY: default build clean darwin-amd64 linux-amd64 all test cov

build: clean
	go build -o ${NAME} ${ENTRIES}


release-build: clean
	go build ${BUILD_OPTS} ${GCFLAGS} -o ${NAME} ${ENTRIES}

clean:
	@rm -rf ${BINDIR} || true


darwin-amd64: clean
	GOARCH=amd64 GOOS=darwin go build ${BUILD_OPTS} -o ${BINDIR}/${NAME}-${DARWIN_AMD64} ${ENTRIES}

_darwin-amd64:
	GOARCH=amd64 GOOS=darwin go build ${BUILD_OPTS} -o ${BINDIR}/${NAME}-${DARWIN_AMD64} ${ENTRIES}

linux-amd64: clean
	GOARCH=amd64 GOOS=linux go build ${BUILD_OPTS} -o ${BINDIR}/${NAME}-${LINUX_AMD64} ${ENTRIES}

_linux-amd64:
	GOARCH=amd64 GOOS=linux go build ${BUILD_OPTS} -o ${BINDIR}/${NAME}-${LINUX_AMD64} ${ENTRIES}

all: clean _linux-amd64 _darwin-amd64

test:
	go test ./...

cov:
	go test ./... -coverpkg=./...

default: build

