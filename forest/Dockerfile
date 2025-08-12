FROM docker.io/rust:1.81-bookworm AS builder

# Pin forest to a specific branch
ARG GIT_COMMIT="main"
ARG LOCAL_BUILD=1
# Step 1: Install dependencies
RUN apt-get update && \
    apt-get install --no-install-recommends jq -y build-essential clang curl git ca-certificates && \
    update-ca-certificates && \
    ARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/') && \
    curl -OL "https://golang.org/dl/go1.23.2.linux-${ARCH}.tar.gz" && \
    tar -C /usr/local -xzf "go1.23.2.linux-${ARCH}.tar.gz" && \
    rm "go1.23.2.linux-${ARCH}.tar.gz"

ENV PATH="/usr/local/go/bin:${PATH}"
# Step 2: Install Rust (this is cached unless Rust changes)
RUN curl https://sh.rustup.rs -sSf | sh -s -- -y --no-modify-path --profile minimal
ENV PATH="/root/.cargo/bin:${PATH}" 
# Step 3: Clone the repository (this is cached unless the repository changes)
RUN git clone https://github.com/chainsafe/forest /forest
# After Go installation and before Rust build
WORKDIR /forest/f3-sidecar
RUN go mod tidy
WORKDIR /forest
RUN git checkout ${GIT_COMMIT}

COPY ./forest_config.toml.tpl .
# Step 4: Copy and apply the patch (this changes more frequently, so it's placed later)
# COPY ./forest.patch ./forest.patch
# RUN git apply ./forest.patch
COPY libvoidstar.so /usr/local/lib/libvoidstar.so 
RUN if [ "$LOCAL_BUILD" = "1" ]; then \
        make install-quick; \
    else \
        # Build forest binaries + Rust instrumentation on main forest binary for code coverage 
        cargo build --release --bin forest-cli && \
        cargo build --release --bin forest-tool && \
        cargo build --release --bin forest-wallet && \
        cp /forest/target/release/forest-cli /usr/local/cargo/bin/forest-cli && \
        cp /forest/target/release/forest-tool /usr/local/cargo/bin/forest-tool && \
        cp /forest/target/release/forest-wallet /usr/local/cargo/bin/forest-wallet && \
        # Setup Antithesis instrumentation
        export LD_LIBRARY_PATH=/usr/local/lib && \
        export RUSTFLAGS="-Ccodegen-units=1 -Cpasses=sancov-module -Cllvm-args=-sanitizer-coverage-level=3 \
        -Cllvm-args=-sanitizer-coverage-trace-pc-guard -Clink-arg=-Wl,-z,undefs \
        -Clink-args=-Wl,--build-id -L/usr/local/lib -lvoidstar" && \
        cd /forest && \
        sed -i 's/strip = true/strip = false/' Cargo.toml && \
        cargo build --release --bin forest && \
        cp /forest/target/release/forest /usr/local/cargo/bin/forest && \
        cd / && \
        mkdir -p /symbols && \
        ln -s /usr/local/cargo/bin/forest /symbols/forest && \
        ldd /symbols/forest | grep "libvoidstar" && \
        nm /symbols/forest | grep "sanitizer_cov_trace_pc_guard"; \
    fi

WORKDIR /forest

# Step 6: Set up environment variables and fetch necessary parameters
ENV FIL_PROOFS_PARAMETER_CACHE="/var/tmp/filecoin-proof-parameters"
RUN forest-tool fetch-params --keys

# Step 7: Cache actor bundle
ENV FOREST_ACTOR_BUNDLE_PATH="/var/tmp/forest_actor_bundle.car.zst"
RUN forest-tool state-migration actor-bundle $FOREST_ACTOR_BUNDLE_PATH

# Step 8: Keep the container alive
ENTRYPOINT ["sleep", "infinity"]
