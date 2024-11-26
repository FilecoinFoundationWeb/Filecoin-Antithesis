# Start with the latest Ubuntu image
FROM ubuntu:latest

# Install necessary packages including Python and Go
RUN apt-get update -y && \
    apt-get install -y python3 python3-pip python3-requests python3-filelock curl jq && \
    curl -OL https://golang.org/dl/go1.23.2.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go1.23.2.linux-amd64.tar.gz && \
    rm go1.23.2.linux-amd64.tar.gz

# Set up Go environment
ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/go"

# Create directories for Go code
WORKDIR /opt/antithesis

# Copy the necessary Go files and resources
COPY ./resources /opt/antithesis/resources
COPY go.mod ./go.mod
COPY go.sum ./go.sum
COPY main.go ./main.go
COPY ./main /opt/antithesis/test/v1/main
COPY ./main/parallel_driver_spammer.py /opt/antithesis/test/v1/main/parallel_driver_spammer.py
COPY ./main/parallel_driver_create_wallets.py /opt/antithesis/test/v1/main/parallel_driver_create_wallets.py
COPY ./main/parallel_driver_delete_wallets.py /opt/antithesis/test/v1/main/parallel_driver_delete_wallets.py
COPY ./main/anytime_increasing_block_height_check.py /opt/antithesis/test/v1/main/anytime_increasing_block_height_check.py
COPY ./main/eventually_all_node_sync_status_check.py /opt/antithesis/test/v1/main/eventually_all_node_sync_status_check.py

COPY ./sdk/antithesis_sdk.py /opt/antithesis/sdk/antithesis_sdk.py

# Download Go dependencies
RUN go mod download

# Build the Go application
RUN go build -o /opt/antithesis/app ./main.go

RUN rm ./main.go

# Set the entrypoint to an idle state
ENTRYPOINT ["sleep", "infinity"]

