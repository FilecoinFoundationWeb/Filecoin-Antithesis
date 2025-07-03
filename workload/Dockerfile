# Start with the latest Ubuntu image
FROM ubuntu:latest

# Note: This container requires CAP_SYS_TIME capability to sync time
# Run with: docker run --cap-add SYS_TIME ...

# Install necessary packages including Python and Go
RUN apt-get update -y && \
    apt-get install -y python3 python3-pip python3-requests python3-filelock curl jq \
    ntpdate git build-essential pkg-config mesa-opencl-icd ocl-icd-opencl-dev \
    hwloc libhwloc-dev && \
    curl -OL https://golang.org/dl/go1.23.2.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go1.23.2.linux-amd64.tar.gz && \
    rm go1.23.2.linux-amd64.tar.gz

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

# Create directories for Go code and smart contracts
WORKDIR /opt/antithesis
RUN git clone https://github.com/FilOzone/pdp.git pdp 
RUN cd pdp && make build

WORKDIR /opt/antithesis/
RUN git clone https://github.com/FilOzone/filecoin-services-payments.git payments
RUN cd payments && forge build

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
RUN go build -o ./app ./main.go

RUN rm ./main.go

# Verify Foundry installation
RUN forge --version && \
    cast --version && \
    anvil --version

# Set the entrypoint to an idle state
ENTRYPOINT ["/opt/antithesis/entrypoint/entrypoint.sh"]
