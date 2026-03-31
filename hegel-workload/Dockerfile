# Stage 1: Build the Rust binary
FROM rust:1.86 AS builder
WORKDIR /build
COPY Cargo.toml Cargo.lock ./
COPY src/ ./src/
RUN cargo build --release --features antithesis

# Stage 2: Runtime with Python 3 + uv (required by Hegel's hegel-core Python backend)
FROM ubuntu:24.04
RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 \
    python3-venv \
    curl \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Install uv (Python package manager used by Hegel to auto-install hegel-core)
RUN curl -LsSf https://astral.sh/uv/install.sh | sh
ENV PATH="/root/.local/bin:${PATH}"

# Copy the built binary
COPY --from=builder /build/target/release/hegel-workload /usr/local/bin/hegel-workload

# Copy entrypoint
COPY entrypoint.sh /opt/antithesis/entrypoint.sh
RUN chmod +x /opt/antithesis/entrypoint.sh

ENTRYPOINT ["/opt/antithesis/entrypoint.sh"]
