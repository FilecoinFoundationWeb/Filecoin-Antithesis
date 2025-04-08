# Dockerfile to build instrumented version of lotus
FROM docker.io/golang:1.23.7-bookworm

# Installing rust
RUN apt-get update && \
    apt-get install -y ca-certificates build-essential clang ocl-icd-opencl-dev ocl-icd-libopencl1 jq libhwloc-dev git && \
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
ENV PATH="/root/.cargo/bin:${PATH}"

ARG GIT_BRANCH="42d68a5"

RUN git clone https://github.com/filecoin-project/lotus.git /lotus
WORKDIR /lotus
RUN latest_version=$(git tag -l 'v*' | sort -V -r | head -n 1)

COPY ./lotus-mal.patch ./lotus-mal.patch
RUN git apply lotus-mal.patch

# COPY ./make.patch ./make.patch
# RUN git apply make.patch

COPY ./config-3.toml .

RUN git submodule update --init

# Perform code coverage instrumentation (source transformation)
RUN go get github.com/antithesishq/antithesis-sdk-go@latest
RUN go get github.com/filecoin-project/go-jsonrpc@v0.7.1
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

# Check lotus version
CMD ["./lotus","--version"]

# Fetch parameters
RUN ./lotus fetch-params 2048

RUN  make install

ENTRYPOINT ["sleep", "infinity"]