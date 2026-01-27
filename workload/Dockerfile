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
RUN ls
# Install Node.js and npm
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && \
    apt-get install -y nodejs && \
    npm install -g npm@latest
RUN npm install -g pnpm@latest
# Install Rust and Foundry
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y && \
    . $HOME/.cargo/env && \
    curl -L https://foundry.paradigm.xyz | bash && \
    . ~/.bashrc && \
    foundryup

# Add Foundry to PATH
ENV PATH="/root/.foundry/bin:${PATH}"
RUN pip install antithesis cffi --break-system-packages

# Set Go environment
ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/go"
RUN npm install -g solc
ENV PATH="$GOPATH/bin:$PATH"
# Install Hardhat (as some projects might need it)

# Install Solidity compiler

# Install abigen using Go (instead of problematic PPA)
RUN go install github.com/ethereum/go-ethereum/cmd/abigen@latest
WORKDIR /opt/antithesis 
RUN npm install -g hardhat
# Clone and build filwizard
RUN pwd
RUN git clone https://github.com/parthshah1/FilWizard /opt/antithesis/FilWizard


WORKDIR /opt/antithesis/FilWizard
RUN git pull
# Install forge dependencies:
RUN git checkout foc-testing
RUN make build

RUN cp ./filwizard /usr/local/bin/filwizard
RUN filwizard contract clone-config --config /opt/antithesis/FilWizard/config/filecoin-synapse.json --workspace ./workspace 
RUN git clone https://github.com/FilOzone/synapse-sdk /opt/antithesis/synapse-sdk
WORKDIR /opt/antithesis/synapse-sdk
RUN git pull
RUN git checkout 8aafdf1c0b8ba1b729898898aec4aeb47f5ac6a4

# Copy and apply synapse-sdk patch
COPY ./patches/synapse-sdk.patch /tmp/synapse-sdk.patch
RUN git apply /tmp/synapse-sdk.patch

ENV NODE_OPTIONS="--max-old-space-size=8192"
ENV CI=true

# Install all workspace dependencies at once
RUN pnpm install

# Build core first (dependency of SDK)
RUN pnpm --filter @filoz/synapse-core run build

# Build SDK
RUN pnpm --filter @filoz/synapse-sdk run build

# Install ethers at root for utils scripts
RUN pnpm add -w ethers
# Build SDK
RUN pnpm --filter @filoz/synapse-sdk run build
RUN ls
WORKDIR /opt/antithesis 
COPY ./entrypoint ./entrypoint
COPY ./main ./test/v1/main

# Copy Go source files for workload binary
COPY ./resources ./resources
COPY go.mod ./go.mod
COPY go.sum ./go.sum
COPY main.go ./main.go

# Make all files in ./test/v1/main executable
RUN chmod +x ./test/v1/main/*

# Download Go dependencies
RUN go mod download

# Build the Go workload binary
ENV CGO_ENABLED=1
RUN GOARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/') go build -o ./workload ./main.go

# Clean up source file after build
RUN rm ./main.go

WORKDIR /opt/antithesis/FilWizard
ENTRYPOINT ["/opt/antithesis/entrypoint/entrypoint.sh"]