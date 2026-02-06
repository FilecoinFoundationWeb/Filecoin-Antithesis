ARG GO_VERSION=1.25.1

FROM golang:$GO_VERSION

# installing jq, opencl, and hwloc (required for make 2k step)
RUN apt-get update && apt-get install -y jq libhwloc-dev ocl-icd-opencl-dev && rm -rf /var/lib/apt/lists/*

# cloning lotus
ARG REF="master"
RUN git clone --depth=1 https://github.com/filecoin-project/lotus.git --branch=${REF} /lotus
WORKDIR /lotus

# copy & apply lotus patch (local drand; setting bootstrap and finality epoch)
COPY patches/lotus.patch lotus.patch
RUN git apply lotus.patch

# copy & apply make patch (builds binaries with race detection)
COPY patches/make.patch make.patch
RUN git apply make.patch

# required to initalize the submodules. we need their go.mod files to exist before we perform the go get below.
RUN git submodule update --init --recursive

# install instrumentor
RUN go get github.com/antithesishq/antithesis-sdk-go@latest
RUN go install github.com/antithesishq/antithesis-sdk-go/tools/antithesis-go-instrumentor@latest
RUN go mod tidy

# compile lotus with instrumentor
RUN mkdir /lotus_instrumented
COPY ./exclusion.txt /exclusion.txt
RUN antithesis-go-instrumentor -exclude /exclusion.txt /lotus /lotus_instrumented
RUN mkdir -p /symbols
RUN cp -r /lotus_instrumented/symbols/* /symbols
WORKDIR /lotus_instrumented/customer/

# deleting original source code
RUN rm -rf /lotus

# install runtime binaries (lotus-shed used in curio) (not using: 2k-lotus-worker)
RUN make 2k-lotus 2k-lotus-miner 2k-lotus-seed 2k-lotus-shed

# moving the binaries (with the same permissions) to /usr/local/bin so that we can run them globally
RUN install -m 0755 lotus lotus-miner lotus-seed lotus-shed /usr/local/bin/

# building binaries to check health and benchmarks
RUN go build -o lotus-health ./cmd/lotus-health
RUN go build -o lotus-bench ./cmd/lotus-bench

# download parameters needed for proving sectors
RUN ./lotus fetch-params 2048

# copy toml config
COPY lotus-config.toml.template config.toml.template

# copy entrypoint scripts
COPY scripts/start-lotus.sh ./scripts/start-lotus.sh
COPY scripts/setup-genesis.sh ./scripts/setup-genesis.sh
COPY scripts/start-lotus-miner.sh ./scripts/start-lotus-miner.sh

# sleep infinity
ENTRYPOINT ["sleep", "infinity"]
