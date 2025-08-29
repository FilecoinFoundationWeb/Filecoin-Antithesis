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

# Install Solidity compiler (solc)
RUN curl -L https://github.com/ethereum/solidity/releases/download/v0.8.24/solc-linux -o /usr/local/bin/solc && \
    chmod +x /usr/local/bin/solc

# Install Foundry (includes forge and cast)
RUN curl -L https://foundry.paradigm.xyz | bash && \
    . /root/.bashrc && \
    foundryup

# Add Foundry binaries to PATH
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
#COPY ./go-test-scripts ./go-test-scripts

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

# Set the entrypoint to an idle state
ENTRYPOINT ["/opt/antithesis/entrypoint/entrypoint.sh"]
