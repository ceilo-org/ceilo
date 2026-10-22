.PHONY: build run test vet fmt clean

VERSION ?= dev


build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/ceilo ./cmd/ceilo

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