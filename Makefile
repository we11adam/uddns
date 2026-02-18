NAME=uddns
ENTRIES=main.go
BINDIR=bin
GIT_REV=$$(git rev-parse HEAD)
BUILD_TIME=$$(date -Iseconds)


BUILD_OPTS=-trimpath
LDFLAGS+=-s -w

BUILD_OPTS+=-ldflags="${LDFLAGS}"

.PHONY: default build clean darwin-amd64 linux-amd64 all test cov

build: clean
	go build -o ${BINDIR}/${NAME} ${ENTRIES}

release-build: clean
	go build ${BUILD_OPTS} ${GCFLAGS} -o ${NAME} ${ENTRIES}

clean:
	@rm -rf ${BINDIR} || true

darwin-amd64:
	GOARCH=amd64 GOOS=darwin go build ${BUILD_OPTS} -o ${BINDIR}/${NAME}-darwin-amd64 ${ENTRIES}

darwin-arm64:
	GOARCH=arm64 GOOS=darwin go build ${BUILD_OPTS} -o ${BINDIR}/${NAME}-darwin-arm64 ${ENTRIES}

linux-amd64:
	GOARCH=amd64 GOOS=linux go build ${BUILD_OPTS} -o ${BINDIR}/${NAME}-linux-amd64 ${ENTRIES}

linux-arm64:
	GOARCH=arm64 GOOS=linux go build ${BUILD_OPTS} -o ${BINDIR}/${NAME}-linux-arm64 ${ENTRIES}

all: clean linux-amd64 linux-arm64 darwin-amd64 darwin-arm64

test:
	go test ./...

cov:
	go test ./... -coverpkg=./...

default: build

