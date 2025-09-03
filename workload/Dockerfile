# Start with the latest Ubuntu image
FROM ubuntu:latest

# Note: This container requires CAP_SYS_TIME capability to sync time
# Run with: docker run --cap-add SYS_TIME ...

# Install necessary packages including Python, Go, Node.js, and Ethereum tools
RUN apt-get update -y && \
    apt-get install -y python3 python3-pip python3-requests python3-filelock curl jq gcc ntpdate git build-essential && \
    ARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/') && \
    curl -OL "https://golang.org/dl/go1.23.2.linux-${ARCH}.tar.gz" && \
    tar -C /usr/local -xzf "go1.23.2.linux-${ARCH}.tar.gz" && \
    rm "go1.23.2.linux-${ARCH}.tar.gz" && \
    # Install Node.js and npm
    curl -fsSL https://deb.nodesource.com/setup_20.x | bash - && \
    apt-get install -y nodejs && \
    # Install yarn
    npm install -g yarn && \
    # Install Foundry (forge, cast, anvil, chisel)
    curl -L https://foundry.paradigm.xyz | bash && \
    /root/.foundry/bin/foundryup && \
    # Install hardhat globally
    npm install -g hardhat

# Install Antithesis Python SDK
RUN pip install antithesis cffi --break-system-packages

# Set up Go and Foundry environment
ENV PATH="/usr/local/go/bin:/root/.foundry/bin:${PATH}"
ENV GOPATH="/go"

# Create directories for Go code
WORKDIR /opt/antithesis


RUN git clone https://github.com/parthshah1/fevm-kit.git /opt/antithesis/projects/fevm-hardhat-kit
RUN git clone https://github.com/parthshah1/stablecoin-contracts.git /opt/antithesis/projects/stablecoin-contracts

WORKDIR /opt/antithesis/projects/fevm-hardhat-kit

RUN yarn install

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

# Verify all tools are installed
RUN echo "Verifying tool installations:" && \
    go version && \
    node --version && \
    npm --version && \
    yarn --version && \
    forge --version && \
    cast --version && \
    hardhat --version

RUN rm ./main.go

# Set the entrypoint to an idle state
ENTRYPOINT ["/opt/antithesis/entrypoint/entrypoint.sh"]
