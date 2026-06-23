BINARIES := dispatcher gatherer researcher planner designer coder committer reviewer iterator feedback eval eval-runner

.PHONY: build clean $(BINARIES)

build: $(BINARIES)

$(BINARIES):
	CGO_ENABLED=0 go build -o bin/$@ ./cmd/$@/

clean:
	rm -rf bin/

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l .

check: fmt vet test

.PHONY: smoke-test smoke-test-ci smoke-test-full

smoke-test-ci:
	go test -race ./tests/ -run TestSmoke -v -timeout 2m

smoke-test:
	@mkdir -p bin
	go build -o bin/smoke-test ./cmd/smoke-test/
	./bin/smoke-test -mode full-mock

smoke-test-full:
	@mkdir -p bin
	go build -o bin/smoke-test ./cmd/smoke-test/
	./bin/smoke-test -mode full -config $(CONFIG)

REGISTRY ?= ghcr.io/ruromero

.PHONY: sandbox-base sandbox-go sandbox-rust sandbox-typescript sandbox-images

sandbox-base:
	docker build -f deploy/sandbox-images/base/Dockerfile -t $(REGISTRY)/factory-base:latest .

sandbox-go: sandbox-base
	docker build --build-arg BASE_IMAGE=$(REGISTRY)/factory-base:latest -f deploy/sandbox-images/go/Dockerfile -t $(REGISTRY)/factory-go:latest .

sandbox-rust: sandbox-base
	docker build --build-arg BASE_IMAGE=$(REGISTRY)/factory-base:latest -f deploy/sandbox-images/rust/Dockerfile -t $(REGISTRY)/factory-rust:latest .

sandbox-typescript: sandbox-base
	docker build --build-arg BASE_IMAGE=$(REGISTRY)/factory-base:latest -f deploy/sandbox-images/typescript/Dockerfile -t $(REGISTRY)/factory-typescript:latest .

sandbox-images: sandbox-go sandbox-rust sandbox-typescript
