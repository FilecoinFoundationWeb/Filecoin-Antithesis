FROM docker.io/golang:1.23.2-bookworm

RUN apt-get update && \
   apt-get install -y ca-certificates build-essential clang ocl-icd-opencl-dev ocl-icd-libopencl1 jq libhwloc-dev git

ARG GIT_BRANCH="v2.1.3"
ARG LOCAL_BUILD=1
RUN git clone --depth=1 --branch="${GIT_BRANCH}" https://github.com/drand/drand /drand

WORKDIR /drand 
RUN git submodule update --init

RUN if [ "$LOCAL_BUILD" = "1" ]; then \
        make build_insecure && \
        cp drand /usr/local/bin/drand; \
    else \
        # Perform instrumentation for non-local builds
        go get github.com/antithesishq/antithesis-sdk-go@latest && \
        go get github.com/filecoin-project/go-jsonrpc@v0.8.0 && \
        go install github.com/antithesishq/antithesis-sdk-go/tools/antithesis-go-instrumentor@latest && \
        go mod tidy && \
        mkdir /drand_instrumented && \
        cp ./exclusion.txt /exclusion.txt && \
        antithesis-go-instrumentor -exclude /exclusion.txt /drand /drand_instrumented && \
        mkdir -p /symbols && \
        cp -r /drand_instrumented/symbols/* /symbols && \
        cd /drand_instrumented/customer/ && \
        rm -rf /drand && \
        make build_insecure && \
        cp drand /usr/local/bin/drand; \
    fi

ENTRYPOINT ["sleep", "infinity"]


