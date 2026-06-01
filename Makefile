BINARIES := dispatcher gatherer researcher planner designer coder committer reviewer iterator eval

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

REGISTRY ?= quay.io/ruromero

.PHONY: sandbox-base sandbox-go sandbox-rust sandbox-typescript sandbox-images

sandbox-base: build
	docker build -t $(REGISTRY)/factory-base:latest deploy/sandbox-images/base/

sandbox-go: sandbox-base
	docker build --build-arg BASE_IMAGE=$(REGISTRY)/factory-base:latest -t $(REGISTRY)/factory-go:latest deploy/sandbox-images/go/

sandbox-rust: sandbox-base
	docker build --build-arg BASE_IMAGE=$(REGISTRY)/factory-base:latest -t $(REGISTRY)/factory-rust:latest deploy/sandbox-images/rust/

sandbox-typescript: sandbox-base
	docker build --build-arg BASE_IMAGE=$(REGISTRY)/factory-base:latest -t $(REGISTRY)/factory-typescript:latest deploy/sandbox-images/typescript/

sandbox-images: sandbox-go sandbox-rust sandbox-typescript
