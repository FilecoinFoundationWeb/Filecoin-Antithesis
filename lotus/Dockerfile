FROM golang:latest

RUN apt-get update && \
    apt-get install -y ca-certificates build-essential clang ocl-icd-opencl-dev ocl-icd-libopencl1 jq libhwloc-dev git && \
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
ENV PATH="/root/.cargo/bin:${PATH}"
    
RUN git clone --single-branch -b releases https://github.com/filecoin-project/lotus.git lotus-local-net
WORKDIR lotus-local-net
COPY ./lotus.patch ./lotus.patch
RUN git apply lotus.patch
WORKDIR lotus-local-net


RUN make -C /go/lotus-local-net 2k
RUN /go/lotus-local-net/./lotus fetch-params 2048
RUN /go/lotus-local-net/./lotus-seed pre-seal --sector-size 2KiB --num-sectors 2
RUN /go/lotus-local-net/./lotus-seed genesis new localnet.json
RUN /go/lotus-local-net/./lotus-seed genesis add-miner localnet.json /root/.genesis-sectors/pre-seal-t01000.json
