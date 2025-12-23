FROM golang:1.25-bookworm

# Install common development tools and build dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    curl \
    ripgrep \
    libgpgme-dev \
    libbtrfs-dev \
    libdevmapper-dev \
    pkg-config \
    && rm -rf /var/lib/apt/lists/*

# Install just
RUN curl --proto '=https' --tlsv1.2 -sSf https://just.systems/install.sh | bash -s -- --to /usr/local/bin

COPY Justfile .
RUN just bootstrap
COPY .agents/sandbox/bashrc /root/.bashrc
CMD ["just install && /bin/bash"]
