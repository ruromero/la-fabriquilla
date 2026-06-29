BINARIES := dispatcher gatherer researcher planner designer coder committer reviewer iterator validator feedback eval eval-runner

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

smoke-test-full: build
	@mkdir -p bin
	go build -o bin/smoke-test ./cmd/smoke-test/
	PATH=$(PWD)/bin:$$PATH ./bin/smoke-test -mode full -config $(CONFIG)

REGISTRY ?= ghcr.io/ruromero
CONTAINER_ENGINE ?= $(shell command -v podman 2>/dev/null || echo docker)

.PHONY: sandbox-base sandbox-go sandbox-rust sandbox-typescript sandbox-images

sandbox-base:
	$(CONTAINER_ENGINE) build -f deploy/sandbox-images/base/Dockerfile -t $(REGISTRY)/factory-base:latest .

sandbox-go: sandbox-base
	$(CONTAINER_ENGINE) build --build-arg BASE_IMAGE=$(REGISTRY)/factory-base:latest -f deploy/sandbox-images/go/Dockerfile -t $(REGISTRY)/factory-go:latest .

sandbox-rust: sandbox-base
	$(CONTAINER_ENGINE) build --build-arg BASE_IMAGE=$(REGISTRY)/factory-base:latest -f deploy/sandbox-images/rust/Dockerfile -t $(REGISTRY)/factory-rust:latest .

sandbox-typescript: sandbox-base
	$(CONTAINER_ENGINE) build --build-arg BASE_IMAGE=$(REGISTRY)/factory-base:latest -f deploy/sandbox-images/typescript/Dockerfile -t $(REGISTRY)/factory-typescript:latest .

sandbox-images: sandbox-go sandbox-rust sandbox-typescript

.PHONY: image image-push deploy

image:
	$(CONTAINER_ENGINE) build -t $(REGISTRY)/fabriquilla:latest .

image-push: image
	$(CONTAINER_ENGINE) push $(REGISTRY)/fabriquilla:latest

deploy:
	@kubectl get secret -n fabriquilla fabriquilla-creds >/dev/null 2>&1 || \
		(echo "Error: Secret 'fabriquilla-creds' not found. Create it first — see deploy/k8s/secret.example.yaml" && exit 1)
	kubectl apply -f deploy/k8s/namespace.yaml
	kubectl apply -f deploy/k8s/configmap.yaml
	kubectl apply -f deploy/k8s/pvc.yaml
	kubectl apply -f deploy/k8s/deployment.yaml
