FROM docker.io/golang:1.23.2-bookworm

RUN apt-get update && \
   apt-get install -y ca-certificates build-essential clang ocl-icd-opencl-dev ocl-icd-libopencl1 jq libhwloc-dev git

ARG GIT_BRANCH="v2.1.2"

RUN git clone --depth=1 --branch="${GIT_BRANCH}" https://github.com/drand/drand

WORKDIR drand 
RUN go install -tags=conn_insecure ./cmd/drand
