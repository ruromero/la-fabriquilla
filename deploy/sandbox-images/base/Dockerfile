FROM ubuntu:24.04

RUN apt-get update && apt-get install -y \
    git gh curl python3 python3-pip jq \
    && rm -rf /var/lib/apt/lists/*

RUN pip3 install --break-system-packages serena

COPY bin/* /usr/local/bin/
