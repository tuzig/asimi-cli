FROM golang:1.23-bullseye

RUN apt-get update && \
    apt-get install -y --no-install-recommends just git && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /workspace
ENV GO111MODULE=on
