# Start with the latest Ubuntu image
FROM ubuntu:latest

# Install necessary packages including Python and Go
RUN apt-get update -y && \
    apt-get install -y python3 python3-pip python3-requests python3-filelock curl jq && \
    curl -OL https://golang.org/dl/go1.23.2.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go1.23.2.linux-amd64.tar.gz && \
    rm go1.23.2.linux-amd64.tar.gz

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
COPY go.mod ./go.mod
COPY go.sum ./go.sum
COPY main.go ./main.go
COPY ./main ./test/v1/main

# Download Go dependencies
RUN go mod download

# Build the Go application
RUN go build -o ./app ./main.go

RUN rm ./main.go

# Set the entrypoint to an idle state
ENTRYPOINT ["/opt/antithesis/entrypoint/entrypoint.sh"]
