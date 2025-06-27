FROM docker.io/rust:1.81-bookworm as builder

# Pin forest to a specific branch
ARG GIT_COMMIT="3894679"

# Step 1: Install dependencies
RUN apt-get update && \
    apt-get install --no-install-recommends jq -y build-essential clang curl git ca-certificates && \
    update-ca-certificates
RUN curl -OL https://golang.org/dl/go1.23.1.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go1.23.1.linux-amd64.tar.gz && \
    rm go1.23.1.linux-amd64.tar.gz
ENV PATH="/usr/local/go/bin:${PATH}"
# Step 2: Install Rust (this is cached unless Rust changes)
RUN curl https://sh.rustup.rs -sSf | sh -s -- -y --no-modify-path --profile minimal
ENV PATH="/root/.cargo/bin:${PATH}" 
# Step 3: Clone the repository (this is cached unless the repository changes)
RUN git clone https://github.com/chainsafe/forest /forest
WORKDIR /forest
RUN git checkout ${GIT_COMMIT}

COPY ./forest_config.toml.tpl .
# Step 4: Copy and apply the patch (this changes more frequently, so it's placed later)
# COPY ./forest.patch ./forest.patch
# RUN git apply ./forest.patch
# Step 5: make install + Rust instrumentation for code coverage 
RUN make install
COPY libvoidstar.so /usr/local/lib/libvoidstar.so
ENV LD_LIBRARY_PATH=/usr/local/lib
ENV RUSTFLAGS="-Ccodegen-units=1 -Cpasses=sancov-module -Cllvm-args=-sanitizer-coverage-level=3 \ 
-Cllvm-args=-sanitizer-coverage-trace-pc-guard -Clink-arg=-Wl,-z,undefs \ 
-Clink-args=-Wl,--build-id -L/usr/local/lib -lvoidstar"
# Set Antithesis runtime library and rustflags
WORKDIR /forest
RUN sed -i 's/strip = true/strip = false/' Cargo.toml
# change cargo.toml profile for instrumentation
RUN cargo build --release --bin forest
RUN cp /forest/target/release/forest /usr/local/cargo/bin/forest
# Overwrite the forest binary from the make install
WORKDIR /
RUN mkdir /symbols && ln -s /usr/local/cargo/bin/forest /symbols/forest
RUN ldd /symbols/forest | grep "libvoidstar" 
RUN nm /symbols/forest | grep "sanitizer_cov_trace_pc_guard"
# confirm that the binary has correctly linked to the Antithesis runtime library
WORKDIR /forest

# Step 6: Set up environment variables and fetch necessary parameters
ENV FIL_PROOFS_PARAMETER_CACHE="/var/tmp/filecoin-proof-parameters"
RUN forest-tool fetch-params --keys

# Step 7: Cache actor bundle
ENV FOREST_ACTOR_BUNDLE_PATH="/var/tmp/forest_actor_bundle.car.zst"
RUN forest-tool state-migration actor-bundle $FOREST_ACTOR_BUNDLE_PATH

# Step 8: Keep the container alive
ENTRYPOINT ["sleep", "infinity"]