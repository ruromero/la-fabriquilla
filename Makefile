BINARIES := dispatcher gatherer researcher planner designer coder committer reviewer iterator eval eval-runner

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

sandbox-base:
	docker build -f deploy/sandbox-images/base/Dockerfile -t $(REGISTRY)/factory-base:latest .

sandbox-go: sandbox-base
	docker build --build-arg BASE_IMAGE=$(REGISTRY)/factory-base:latest -f deploy/sandbox-images/go/Dockerfile -t $(REGISTRY)/factory-go:latest .

sandbox-rust: sandbox-base
	docker build --build-arg BASE_IMAGE=$(REGISTRY)/factory-base:latest -f deploy/sandbox-images/rust/Dockerfile -t $(REGISTRY)/factory-rust:latest .

sandbox-typescript: sandbox-base
	docker build --build-arg BASE_IMAGE=$(REGISTRY)/factory-base:latest -f deploy/sandbox-images/typescript/Dockerfile -t $(REGISTRY)/factory-typescript:latest .

sandbox-images: sandbox-go sandbox-rust sandbox-typescript
