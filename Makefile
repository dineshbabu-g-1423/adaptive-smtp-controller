.PHONY: all build test vet run sim fmt tidy cover clean

all: vet test build

build:
	go build -o bin/controller ./cmd/controller
	go build -o bin/simulator  ./cmd/simulator

test:
	go test ./... -race -count=1

vet:
	go vet ./...

fmt:
	gofmt -s -w .

tidy:
	go mod tidy

cover:
	go test ./... -coverprofile=coverage.out
	go tool cover -func=coverage.out

run: build
	./bin/controller

sim: build
	./bin/simulator outlook-throttle

clean:
	rm -rf bin coverage.out coverage.html
