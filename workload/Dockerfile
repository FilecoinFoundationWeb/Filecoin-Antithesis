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
RUN pip install antithesis cffi --break-system-packages

# Set Go environment
ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/go"
ENV PATH="$GOPATH/bin:$PATH"

# Install Hardhat (as some projects might need it)
RUN npm install -g hardhat

# Install Solidity compiler
RUN npm install -g solc

# Install abigen using Go (instead of problematic PPA)
RUN go install github.com/ethereum/go-ethereum/cmd/abigen@latest

# Clone and build filwizard
RUN git clone https://github.com/parthshah1/filwizard /opt/antithesis

# Set working directory
WORKDIR /opt/antithesis
RUN git checkout main
COPY ./entrypoint ./entrypoint
COPY ./main ./test/v1/main
# Build filwizard
RUN make build
RUN cp ./filwizard /usr/local/bin/filwizard
RUN filwizard contract clone-config --config config/contracts.json --workspace ./workspace 
# Default command
CMD ["filwizard"]
ENTRYPOINT ["/opt/antithesis/entrypoint/entrypoint.sh"]