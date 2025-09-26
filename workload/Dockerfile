# Start with the latest Ubuntu image
FROM ubuntu:latest

# Note: This container requires CAP_SYS_TIME capability to sync time
# Run with: docker run --cap-add SYS_TIME ...

# Install necessary packages including Python and Go
RUN apt-get update -y && \
    apt-get install -y python3 python3-pip python3-requests python3-filelock curl jq gcc ntpdate git && \
    ARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/') && \
    curl -OL "https://golang.org/dl/go1.23.2.linux-${ARCH}.tar.gz" && \
    tar -C /usr/local -xzf "go1.23.2.linux-${ARCH}.tar.gz" && \
    rm "go1.23.2.linux-${ARCH}.tar.gz"

# Install Node.js and npm
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash - && \
    apt-get install -y nodejs && \
    npm install -g npm@latest

# Install Rust and Foundry
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y && \
    . $HOME/.cargo/env && \
    curl -L https://foundry.paradigm.xyz | bash && \
    . ~/.bashrc && \
    foundryup

# Add Foundry to PATH
ENV PATH="/root/.foundry/bin:${PATH}"

# Install Antithesis Python SDK
RUN pip install antithesis cffi --break-system-packages

# Set up Go environment
ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/go"

# Create directories for Go code
WORKDIR /opt/antithesis

# Copy the necessary Go files and resources
COPY ./entrypoint ./entrypoint
COPY ./resources ./resources
COPY go.mod ./go.mod
COPY go.sum ./go.sum
COPY main.go ./main.go
COPY ./main ./test/v1/main

# Make all files in ./test/v1/main executable
RUN chmod +x ./test/v1/main/*

# Download Go dependencies
RUN go mod download

# Build the Go application
ENV CGO_ENABLED=1
RUN GOARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/') go build -o ./app ./main.go

RUN rm ./main.go
RUN
# Clone and build mpool-tx
RUN git clone https://github.com/parthshah1/mpool-tx.git ./mpool-tx 
WORKDIR /opt/antithesis/mpool-tx
RUN git checkout api-assertions
RUN go mod tidy && make build
# Install mpool-tx binary
RUN cp ./mpool-tx /usr/local/bin/mpool-tx
RUN chmod +x /usr/local/bin/mpool-tx

# Verify installation and run config setup
RUN mpool-tx --version || echo "mpool-tx installed"

# Install abigen (go-ethereum tools)
RUN go install github.com/ethereum/go-ethereum/cmd/abigen@latest && \
    ln -s /go/bin/abigen /usr/local/bin/abigen

# Reset working directory
WORKDIR /opt/antithesis

ENTRYPOINT ["/opt/antithesis/entrypoint/entrypoint.sh"]