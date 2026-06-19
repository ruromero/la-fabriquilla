ARG BASE_IMAGE=factory-base:latest
FROM ${BASE_IMAGE}

COPY --from=golang:1.26-alpine /usr/local/go /usr/local/go

ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/root/go"
ENV PATH="${GOPATH}/bin:${PATH}"

RUN go install golang.org/x/tools/gopls@latest
