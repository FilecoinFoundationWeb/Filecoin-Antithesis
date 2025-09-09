# drand + lotus are considered more stable dependencies
drand_tag = $(shell git ls-remote --tags https://github.com/drand/drand.git | grep -E 'refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$$' | tail -n1 | sed 's/.*refs\/tags\///')
lotus_tag = $(shell git ls-remote https://github.com/filecoin-project/lotus.git HEAD | cut -f1)
builder = docker
forest_commit = $(shell git ls-remote https://github.com/ChainSafe/forest.git HEAD | cut -f1)

# Architecture configuration - set TARGET_ARCH to build for specific architecture
TARGET_ARCH ?= $(shell uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')
DOCKER_PLATFORM = linux/$(TARGET_ARCH)

# Simple build command that works with any architecture
BUILD_CMD = docker build

.PHONY: show-drand-tag
show-drand-tag:
	@echo "Drand tag: $(drand_tag)"

.PHONY: show-lotus-tag
show-lotus-tag:
	@echo "Lotus tag: $(lotus_tag)"

.PHONY: show-forest-commit
show-forest-commit:
	@echo "Forest commit: $(forest_commit)"
.PHONY: build-forest
build-forest:
	@echo "Building forest for $(TARGET_ARCH) architecture..."
	@echo "Forest commit: $(forest_commit)"
	$(BUILD_CMD) --build-arg GIT_COMMIT=$(forest_commit) -t forest:latest -f forest/Dockerfile forest

.PHONY: build-drand
build-drand:
	@echo "Building drand for $(TARGET_ARCH) architecture..."
	@echo "Drand tag: $(drand_tag)"
	$(BUILD_CMD) --build-arg=GIT_BRANCH=$(drand_tag) -t drand:latest -f drand/Dockerfile drand

.PHONY: build-lotus
build-lotus:
	@echo "Building lotus for $(TARGET_ARCH) architecture..."
	@echo "Lotus tag: $(lotus_tag)"
	$(BUILD_CMD) --build-arg=GIT_BRANCH=$(lotus_tag) -t lotus:latest -f lotus/Dockerfile lotus

.PHONY: build-workload
build-workload:
	@echo "Building workload for $(TARGET_ARCH) architecture..."
	$(BUILD_CMD) -t workload:latest -f workload/Dockerfile workload

.PHONY: run-localnet
run-localnet:
	$(builder) compose up

# Build everything and run local docker compose up
.PHONY: all
all: build-drand build-forest build-lotus build-workload run-localnet

# Show current target architecture
.PHONY: show-arch
show-arch:
	@echo "Current target architecture: $(TARGET_ARCH)"
	@echo "Docker platform: $(DOCKER_PLATFORM)"

.PHONY: cleanup
cleanup:
	$(builder) compose down
	bash cleanup.sh

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  all              - Build all images for current architecture ($(TARGET_ARCH)) and run localnet"
	@echo "  build-<service>  - Build specific service (forest, drand, lotus, workload)"
	@echo "  show-arch        - Show current target architecture"
	@echo "  cleanup          - Clean up containers and run cleanup script"
	@echo ""
	@echo "Architecture control:"
	@echo "  make all                  - Build all services for current architecture"