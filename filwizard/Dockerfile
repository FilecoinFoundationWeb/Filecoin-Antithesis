# Stage 1: Clone and build the filwizard binary
FROM golang:latest AS builder

# Clone FilWizard repo (for contract deployment only)
RUN git clone https://github.com/parthshah1/FilWizard /build
WORKDIR /build
RUN git checkout main
RUN git pull

# Build filwizard binary
RUN go mod download
RUN CGO_ENABLED=1 go build -o filwizard .

# Install abigen (go-ethereum ABI binding generator)
RUN go install github.com/ethereum/go-ethereum/cmd/abigen@latest

# Stage 2: Runtime image with all tools needed for contract deployment
FROM ubuntu:24.04

RUN apt-get update && apt-get install -y \
    curl jq git ca-certificates nodejs npm \
    && rm -rf /var/lib/apt/lists/*

# Install pnpm
RUN npm install -g pnpm@latest

# Install Foundry
RUN curl -L https://foundry.paradigm.xyz | bash
ENV PATH="/root/.foundry/bin:${PATH}"
RUN foundryup

# Copy filwizard binary
COPY --from=builder /build/filwizard /usr/local/bin/filwizard

# Copy abigen binary
COPY --from=builder /go/bin/abigen /usr/local/bin/abigen

# Pre-clone and compile FOC contracts (air-gapped operation)
WORKDIR /opt/filwizard
COPY --from=builder /build/config/filecoin-synapse.json /opt/filwizard/config/filecoin-synapse.json

RUN filwizard contract clone-config \
    --config /opt/filwizard/config/filecoin-synapse.json \
    --workspace /opt/filwizard/workspace


# Copy scripts
COPY scripts/register-service-provider.js /opt/filwizard/scripts/register-service-provider.js
COPY scripts/warm-add.js /opt/filwizard/scripts/warm-add.js
COPY scripts/package.json /opt/filwizard/scripts/package.json
COPY scripts/export-environment.sh /opt/filwizard/scripts/export-environment.sh
RUN chmod +x /opt/filwizard/scripts/register-service-provider.js /opt/filwizard/scripts/warm-add.js /opt/filwizard/scripts/export-environment.sh
# Install script dependencies locally (ethers.js)
RUN cd /opt/filwizard/scripts && npm install

WORKDIR /opt/filwizard

# Default entrypoint — will be overridden by compose
ENTRYPOINT ["filwizard"]
