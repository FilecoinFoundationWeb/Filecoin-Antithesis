# ── Stage 1: Build hegel-workload (Rust) ──
FROM rust:1.86 AS rust-builder
WORKDIR /build
COPY hegel-workload/Cargo.toml hegel-workload/Cargo.lock ./
COPY hegel-workload/src/ ./src/
RUN cargo build --release --features antithesis

FROM ubuntu:latest

# Core dependencies only — no Foundry, no Rust, no pnpm, no Hardhat
RUN apt-get update -y && \
    apt-get install -y curl jq gcc ntpdate git python3 python3-venv ca-certificates && \
    ARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/') && \
    curl -OL "https://golang.org/dl/go1.23.2.linux-${ARCH}.tar.gz" && \
    tar -C /usr/local -xzf "go1.23.2.linux-${ARCH}.tar.gz" && \
    rm "go1.23.2.linux-${ARCH}.tar.gz"

ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/go"
ENV PATH="$GOPATH/bin:$PATH"

RUN curl -LsSf https://astral.sh/uv/install.sh | sh
ENV PATH="/root/.local/bin:${PATH}"

WORKDIR /opt/antithesis
COPY workload/ .

# Build Go binaries
RUN go mod download
ENV CGO_ENABLED=1
RUN GOARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/') \
    go build -o ./genesis-prep ./cmd/genesis-prep
RUN GOARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/') \
    go build -o ./stress-engine ./cmd/stress-engine
RUN GOARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/') \
    go build -o ./setup-complete ./cmd/setup-complete
RUN GOARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/') \
    go build -o ./foc-sidecar ./cmd/foc-sidecar
RUN GOARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/') \
    go build -o ./protocol-fuzzer ./cmd/protocol-fuzzer

COPY --from=rust-builder /build/target/release/hegel-workload /usr/local/bin/hegel-workload

# Pre-install hegel-core so the workload doesn't need internet access at runtime.
# HEGEL_SERVER_COMMAND tells hegeltest to use this binary instead of downloading.
RUN uv venv .hegel/venv && uv pip install --python .hegel/venv/bin/python hegel-core==0.2.3
ENV HEGEL_SERVER_COMMAND="/opt/antithesis/.hegel/venv/bin/hegel"

RUN chmod +x ./entrypoint/entrypoint.sh ./entrypoint/entrypoint-hegel.sh

ENTRYPOINT ["/opt/antithesis/entrypoint/entrypoint.sh"]
