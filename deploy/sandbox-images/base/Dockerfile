FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN for bin in dispatcher gatherer researcher planner designer coder committer reviewer iterator; do \
      CGO_ENABLED=0 go build -o /out/$bin ./cmd/$bin/; \
    done

FROM ubuntu:24.04

RUN apt-get update && apt-get install -y --no-install-recommends \
    git gh curl python3 python3-pip jq ca-certificates \
    && rm -rf /var/lib/apt/lists/*

RUN pip3 install --break-system-packages serena

COPY --from=build /out/* /usr/local/bin/
