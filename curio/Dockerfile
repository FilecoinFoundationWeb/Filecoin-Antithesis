# STAGE 1: Use the pre-built lotus image as a source for its binaries
# This avoids rebuilding lotus and uses the same version as the rest of your environment.
ARG LOTUS_TAG=latest
FROM lotus:${LOTUS_TAG} as lotus-builder

FROM docker.io/golang:latest

# First, copy the lotus binaries from the builder stage into our final image
COPY --from=lotus-builder /usr/local/bin /usr/local/bin

# Now, proceed with the Curio build
RUN apt-get update && apt-get install -y --no-install-recommends dnsutils git ca-certificates build-essential clang ocl-icd-opencl-dev ocl-icd-libopencl1 jq libhwloc-dev xxd
ENV XDG_CACHE_HOME="/tmp"

ENV RUSTUP_HOME=/usr/local/rustup \
    CARGO_HOME=/usr/local/cargo \
    PATH=/usr/local/cargo/bin:$PATH \
    RUST_VERSION=1.73.0

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

RUN set -eux; \
    dpkgArch="$(dpkg --print-architecture)"; \
    case "${dpkgArch##*-}" in \
    amd64) rustArch='x86_64-unknown-linux-gnu'; rustupSha256='5cc9ffd1026e82e7fb2eec2121ad71f4b0f044e88bca39207b3f6b769aaa799c' ;; \
    arm64) rustArch='aarch64-unknown-linux-gnu'; rustupSha256='e189948e396d47254103a49c987e7fb0e5dd8e34b200aa4481ecc4b8e41fb929' ;; \
    *) echo >&2 "unsupported architecture: ${dpkgArch}"; exit 1 ;; \
    esac; \
    url="https://static.rust-lang.org/rustup/archive/1.25.1/${rustArch}/rustup-init"; \
    wget --quiet "$url"; \
    echo "${rustupSha256} *rustup-init" | sha256sum -c -; \
    chmod +x rustup-init; \
    ./rustup-init -y --no-modify-path --profile minimal --default-toolchain $RUST_VERSION --default-host ${rustArch}; \
    rm rustup-init; \
    chmod -R a+w $RUSTUP_HOME $CARGO_HOME; \
    rustup --version; \
    cargo --version; \
    rustc --version;

RUN git clone https://github.com/filecoin-project/curio.git /opt/curio

WORKDIR /opt/curio
RUN git checkout main

### make configurable filecoin-ffi build
ARG FFI_BUILD_FROM_SOURCE=0
ENV FFI_BUILD_FROM_SOURCE=${FFI_BUILD_FROM_SOURCE}

RUN make clean deps

ARG RUSTFLAGS=""
ARG GOFLAGS=""

RUN make 2k
RUN make pdptool
RUN make sptool
RUN install -C curio /usr/local/bin/curio
RUN install -C pdptool /usr/local/bin/pdptool
RUN install -C sptool /usr/local/bin/sptool
RUN curio fetch-params 2048
ENTRYPOINT ["sleep", "infinity"] 