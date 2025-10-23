FROM golang:1.24-bullseye

RUN apt-get update && \
    apt-get install -y --no-install-recommends git curl && \
    rm -rf /var/lib/apt/lists/*

# Install just from GitHub releases
RUN JUST_VERSION=1.40.0 && \
    ARCH=$(dpkg --print-architecture) && \
    curl -L "https://github.com/casey/just/releases/download/${JUST_VERSION}/just-${JUST_VERSION}-$(uname -m)-unknown-linux-musl.tar.gz" | \
    tar xz -C /usr/local/bin just && \
    chmod +x /usr/local/bin/just

WORKDIR /workspace
ENV GO111MODULE=on
