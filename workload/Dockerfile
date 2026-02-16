FROM ubuntu:latest

# Core dependencies only — no Foundry, no Rust, no pnpm, no Hardhat
RUN apt-get update -y && \
    apt-get install -y python3 python3-pip python3-requests python3-filelock \
    curl jq gcc ntpdate git && \
    pip3 install antithesis cffi --break-system-packages && \
    ARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/') && \
    curl -OL "https://golang.org/dl/go1.23.2.linux-${ARCH}.tar.gz" && \
    tar -C /usr/local -xzf "go1.23.2.linux-${ARCH}.tar.gz" && \
    rm "go1.23.2.linux-${ARCH}.tar.gz"

ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/go"
ENV PATH="$GOPATH/bin:$PATH"

WORKDIR /opt/antithesis
COPY . .

# Build Go binaries
RUN go mod download
ENV CGO_ENABLED=1
RUN GOARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/') \
    go build -o ./genesis-prep ./cmd/genesis-prep
RUN GOARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/') \
    go build -o ./stress-engine ./cmd/stress-engine

RUN chmod +x ./entrypoint/entrypoint.sh

ENTRYPOINT ["/opt/antithesis/entrypoint/entrypoint.sh"]
