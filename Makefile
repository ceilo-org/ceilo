.PHONY: build run test vet fmt clean

VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/ceilo ./cmd/ceilo

# make run ARGS="hello.luau"
run:
	go run ./cmd/ceilo $(ARGS)

test:
	go test ./... -count=1

vet:
	go vet ./...

fmt:
	gofmt -l -w .

clean:
	rm -rf bin