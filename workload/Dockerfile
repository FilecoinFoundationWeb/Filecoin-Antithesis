# Start with the latest Ubuntu image
FROM ubuntu:latest

# Note: This container requires CAP_SYS_TIME capability to sync time
# Run with: docker run --cap-add SYS_TIME ...

# Install necessary packages including Python and Go
RUN apt-get update -y && \
    apt-get install -y curl jq gcc git python3 python3-pip && \
    pip3 install antithesis --break-system-packages && \
    ARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/') && \
    curl -OL "https://golang.org/dl/go1.23.2.linux-${ARCH}.tar.gz" && \
    tar -C /usr/local -xzf "go1.23.2.linux-${ARCH}.tar.gz" && \
    rm "go1.23.2.linux-${ARCH}.tar.gz"

# # Install Node.js and npm
# RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && \
#     apt-get install -y nodejs && \
#     npm install -g npm@latest
# RUN npm install -g pnpm@latest
# # Install Rust and Foundry
# RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y && \
#     . $HOME/.cargo/env && \
#     curl -L https://foundry.paradigm.xyz | bash && \
#     . ~/.bashrc && \
#     foundryup
#
# # Add Foundry to PATH
# ENV PATH="/root/.foundry/bin:${PATH}"

# Set Go environment
ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/go"
ENV PATH="$GOPATH/bin:$PATH"

# # Install abigen using Go (instead of problematic PPA)
# RUN go install github.com/ethereum/go-ethereum/cmd/abigen@latest
WORKDIR /opt/antithesis
# # Clone and build filwizard
# RUN git clone https://github.com/parthshah1/FilWizard /opt/antithesis/FilWizard
# WORKDIR /opt/antithesis/FilWizard
# RUN git pull
# RUN git checkout main
# RUN make build
# RUN cp ./filwizard /usr/local/bin/filwizard
# RUN filwizard contract clone-config --config /opt/antithesis/FilWizard/config/filecoin-synapse.json --workspace ./workspace
# RUN git clone https://github.com/FilOzone/synapse-sdk /opt/antithesis/synapse-sdk
# WORKDIR /opt/antithesis/synapse-sdk
# RUN git pull
# RUN git checkout 8aafdf1c0b8ba1b729898898aec4aeb47f5ac6a4
# COPY ./patches/synapse-sdk.patch /tmp/synapse-sdk.patch
# RUN git apply /tmp/synapse-sdk.patch
# ENV NODE_OPTIONS="--max-old-space-size=8192"
# ENV CI=true
# RUN pnpm install
# RUN pnpm --filter @filoz/synapse-core run build
# RUN pnpm --filter @filoz/synapse-sdk run build
# RUN pnpm add -w ethers
# RUN pnpm --filter @filoz/synapse-sdk run build

WORKDIR /opt/antithesis
COPY . .

# Download Go dependencies
RUN go mod download

# Build both binaries
ENV CGO_ENABLED=1
RUN GOARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/') \
    go build -o ./genesis-prep ./cmd/genesis-prep
RUN GOARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/') \
    go build -o ./stress-engine ./cmd/stress-engine

# Make entrypoint scripts executable
RUN chmod +x ./entrypoint/entrypoint.sh

ENTRYPOINT ["/opt/antithesis/entrypoint/entrypoint.sh"]
