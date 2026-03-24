FROM {{.BaseImage}}

# Install common development tools
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    curl \
    ripgrep \
    && rm -rf /var/lib/apt/lists/*

# Install just (universal build tool)
RUN curl --proto '=https' --tlsv1.2 -sSf https://just.systems/install.sh | bash -s -- --to /usr/local/bin

COPY Justfile .
RUN just bootstrap
COPY .agents/sandbox/bashrc /root/.bashrc

# Default command: install dependencies and start shell
CMD ["just install && /bin/bash"]
