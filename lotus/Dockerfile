# Dockerfile to build instrumented version of lotus
FROM docker.io/golang:1.23.2-bookworm

# Installing rust
RUN apt-get update && \
    apt-get install -y ca-certificates build-essential clang ocl-icd-opencl-dev ocl-icd-libopencl1 jq libhwloc-dev git && \
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
ENV PATH="/root/.cargo/bin:${PATH}"

ARG GIT_BRANCH="v1.29.1"

RUN git clone --depth=1 --branch="${GIT_BRANCH}" https://github.com/filecoin-project/lotus.git /lotus

WORKDIR /lotus
RUN latest_version=$(git tag -l 'v*' | sort -V -r | head -n 1)
COPY ./lotus.patch ./lotus.patch
COPY ./config-node1.toml .
COPY ./config-node2.toml .
RUN git apply lotus.patch

RUN make 2k

CMD ["./lotus","--version"]
# Fetch parameters
RUN ./lotus fetch-params 2048

# Pre-seal for the first miner
RUN ./lotus-seed --sector-dir=/root/.genesis-sectors pre-seal --sector-size 2KiB --num-sectors 2 --miner-addr t01000

#Pre-seal for the second miner
RUN ./lotus-seed --sector-dir=/root/.genesis-sectors2 pre-seal --sector-size 2KiB --num-sectors 3 --miner-addr t01001 
# Create genesis template for first miner
RUN ./lotus-seed genesis new localnet.json
# Create genesis template for second miner 
RUN ./lotus-seed genesis new localnet2.json
# Add the first miner to the genesis template
RUN ./lotus-seed aggregate-manifests "/root/.genesis-sectors/pre-seal-t01000.json" "/root/.genesis-sectors2/pre-seal-t01001.json" > manifest.json
RUN ./lotus-seed genesis add-miner localnet.json manifest.json
# Add the second miner to the genesis template
RUN ./lotus-seed genesis add-miner localnet2.json manifest.json

RUN  make install

ENTRYPOINT ["sleep", "infinity"]
