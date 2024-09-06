FROM docker.io/golang:1.22.4-bookworm as builder

RUN apt-get update && \
    apt-get install --no-install-recommends -y build-essential clang curl git ca-certificates
RUN update-ca-certificates

RUN curl https://sh.rustup.rs -sSf | sh -s -- -y --no-modify-path --profile minimal
ENV PATH="/root/.cargo/bin:${PATH}"

RUN git clone --recursive https://github.com/chainsafe/forest /opt/forest
WORKDIR  /opt/forest

RUN make release

