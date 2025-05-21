FROM mcr.microsoft.com/devcontainers/go:1.22-bullseye

# Install docker-in-docker
RUN apt-get update && apt-get install -y \
    docker.io \
    docker-compose \
    && rm -rf /var/lib/apt/lists/*

# Set up the workspace
RUN mkdir -p /workspace
WORKDIR /workspace

# Set up Go environment
ENV GOPATH=/go
ENV PATH=/go/bin:$PATH

# Create the vscode user
RUN groupadd --gid 1000 vscode \
    && useradd --uid 1000 --gid 1000 --shell /bin/bash --create-home vscode \
    && chown -R vscode:vscode /workspace /go

# Set up SSH
RUN mkdir -p /home/vscode/.ssh
RUN chown vscode:vscode /home/vscode/.ssh

# Switch to vscode user
USER vscode
