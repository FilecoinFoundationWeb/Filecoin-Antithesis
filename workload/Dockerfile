# Start with the latest Ubuntu image
FROM ubuntu:latest

# Note: This container requires CAP_SYS_TIME capability to sync time
# Run with: docker run --cap-add SYS_TIME ...

# Install necessary packages including Python and Go
RUN apt-get update -y && \
    apt-get install -y python3 python3-pip python3-requests python3-filelock curl jq gcc ntpdate && \
    ARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/') && \
    curl -OL "https://golang.org/dl/go1.23.2.linux-${ARCH}.tar.gz" && \
    tar -C /usr/local -xzf "go1.23.2.linux-${ARCH}.tar.gz" && \
    rm "go1.23.2.linux-${ARCH}.tar.gz"

# Install Antithesis Python SDK
RUN pip install antithesis cffi --break-system-packages

# Set up Go environment
ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/go"

# Create directories for Go code
WORKDIR /opt/antithesis
RUN apt install -y git
COPY ./entrypoint ./entrypoint/
RUN git clone https://github.com/parthshah1/filwizard.git /opt/antithesis/filwizard

WORKDIR /opt/antithesis/filwizard
RUN git checkout poc-demo
RUN go build -o /usr/local/bin/filwizard ./main.go 

# Copy the necessary Go files and resources

# Set the entrypoint to an idle state
ENTRYPOINT ["/opt/antithesis/entrypoint/entrypoint.sh"]
