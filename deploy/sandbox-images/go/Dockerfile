FROM factory-base:latest

RUN curl -LO https://go.dev/dl/go1.24.linux-amd64.tar.gz \
    && tar -C /usr/local -xzf go*.tar.gz && rm go*.tar.gz

ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/root/go"
ENV PATH="${GOPATH}/bin:${PATH}"

RUN go install golang.org/x/tools/gopls@latest
