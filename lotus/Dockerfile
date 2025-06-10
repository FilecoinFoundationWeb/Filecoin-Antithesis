# Dockerfile to build instrumented version of lotus
FROM docker.io/golang:1.23.7-bookworm

# Installing rust
RUN apt-get update && \
    apt-get install -y ca-certificates build-essential clang ocl-icd-opencl-dev ocl-icd-libopencl1 jq libhwloc-dev git && \
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
ENV PATH="/root/.cargo/bin:${PATH}"

ARG GIT_BRANCH="feat/f3-cache-changes"

RUN git clone https://github.com/filecoin-project/lotus.git /lotus
WORKDIR /lotus
RUN latest_version=$(git tag -l 'v*' | sort -V -r | head -n 1)

RUN git checkout ${GIT_BRANCH}

COPY ./lotus.patch ./lotus.patch
RUN git apply lotus.patch

# COPY ./make.patch ./make.patch
# RUN git apply make.patch

COPY ./config-1.toml .
COPY ./config-2.toml .

RUN git submodule update --init

# Perform code coverage instrumentation (source transformation)
RUN go get github.com/antithesishq/antithesis-sdk-go@latest
RUN go get github.com/filecoin-project/go-jsonrpc@v0.8.0
RUN go install github.com/antithesishq/antithesis-sdk-go/tools/antithesis-go-instrumentor@latest
RUN go mod tidy
RUN mkdir /lotus_instrumented
COPY ./exclusion.txt /exclusion.txt
RUN antithesis-go-instrumentor -exclude /exclusion.txt /lotus /lotus_instrumented

RUN mkdir -p /symbols
RUN cp -r /lotus_instrumented/symbols/* /symbols

WORKDIR /lotus_instrumented/customer/

# Removing original source code
RUN rm -rf /lotus

# Build 2k binary for lotus
RUN make 2k
RUN go build -o lotus-health ./cmd/lotus-health
RUN go build -o lotus-bench ./cmd/lotus-bench
# Check lotus version
CMD ["./lotus","--version"]

# Fetch parameters
RUN ./lotus fetch-params 2048

# Pre-seal for the first miner
RUN ./lotus-seed --sector-dir=/root/.genesis-sector-1 pre-seal --sector-size 2KiB --num-sectors 2 --miner-addr t01000
#Pre-seal for the second miner
RUN ./lotus-seed --sector-dir=/root/.genesis-sector-2 pre-seal --sector-size 2KiB --num-sectors 3 --miner-addr t01001 
# Create genesis template for first miner
RUN ./lotus-seed genesis new localnet-1.json
# Create genesis template for second miner 
RUN ./lotus-seed genesis new localnet-2.json
# Add the first miner to the genesis template
RUN ./lotus-seed aggregate-manifests "/root/.genesis-sector-1/pre-seal-t01000.json" "/root/.genesis-sector-2/pre-seal-t01001.json" > manifest.json
RUN ./lotus-seed genesis add-miner localnet-1.json manifest.json
# Add the second miner to the genesis template
RUN ./lotus-seed genesis add-miner localnet-2.json manifest.json


RUN make install

ENTRYPOINT ["sleep", "infinity"]
