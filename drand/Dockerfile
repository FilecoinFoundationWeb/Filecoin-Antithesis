FROM docker.io/golang:1.23.2-bookworm

RUN apt-get update && \
   apt-get install -y ca-certificates build-essential clang ocl-icd-opencl-dev ocl-icd-libopencl1 jq libhwloc-dev git

ARG GIT_BRANCH="v2.1.3"

RUN git clone --depth=1 --branch="${GIT_BRANCH}" https://github.com/drand/drand /drand

WORKDIR /drand 
RUN git submodule update --init

RUN go get github.com/antithesishq/antithesis-sdk-go@latest
RUN go get github.com/filecoin-project/go-jsonrpc@v0.8.0
RUN go install github.com/antithesishq/antithesis-sdk-go/tools/antithesis-go-instrumentor@latest
RUN go mod tidy
RUN mkdir /drand_instrumented
RUN antithesis-go-instrumentor /drand /drand_instrumented

RUN mkdir -p /symbols
RUN cp -r /drand_instrumented/symbols/* /symbols

WORKDIR /drand_instrumented/customer/
RUN rm -rf /drand

RUN make build_insecure
RUN cp drand /usr/local/bin/drand