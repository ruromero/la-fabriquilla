FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN set -eux; mkdir -p /out; \
    for bin in dispatcher gatherer researcher planner designer coder \
    committer reviewer iterator feedback smoke-test; do \
      CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/$bin ./cmd/$bin/; \
    done

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/* /usr/local/bin/
ENTRYPOINT ["/usr/local/bin/dispatcher"]
