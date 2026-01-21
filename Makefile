# Filecoin Antithesis Docker Build Makefile
# ==========================================

# Git tags/commits for each image
drand_tag = $(shell git ls-remote --tags https://github.com/drand/drand.git | grep -E 'refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$$' | tail -n1 | sed 's/.*refs\/tags\///')
lotus_tag = $(shell git ls-remote https://github.com/filecoin-project/lotus.git HEAD | cut -f1)
curio_tag = $(shell git ls-remote https://github.com/filecoin-project/curio.git refs/heads/pdpv0 | cut -f1)
forest_commit = $(shell git ls-remote https://github.com/ChainSafe/forest.git HEAD | cut -f1)

# Docker configuration
builder = docker
BUILD_CMD = docker build

# Architecture configuration
TARGET_ARCH ?= $(shell uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')
DOCKER_PLATFORM = linux/$(TARGET_ARCH)

# Build mode: "local" uses local images, "remote" uses Antithesis registry
BUILD_MODE ?= local

# ==========================================
# Show version info
# ==========================================
.PHONY: show-versions
show-versions:
	@echo "Image versions:"
	@echo "  Drand:   $(drand_tag)"
	@echo "  Lotus:   $(lotus_tag)"
	@echo "  Forest:  $(forest_commit)"
	@echo "  Curio:   $(curio_tag)"
	@echo "  Arch:    $(TARGET_ARCH)"

.PHONY: show-drand-tag
show-drand-tag:
	@echo "Drand tag: $(drand_tag)"

.PHONY: show-lotus-tag
show-lotus-tag:
	@echo "Lotus tag: $(lotus_tag)"

.PHONY: show-forest-commit
show-forest-commit:
	@echo "Forest commit: $(forest_commit)"

.PHONY: show-curio-tag
show-curio-tag:
	@echo "Curio tag: $(curio_tag)"

.PHONY: show-arch
show-arch:
	@echo "Current target architecture: $(TARGET_ARCH)"
	@echo "Docker platform: $(DOCKER_PLATFORM)"

# ==========================================
# Build individual images
# ==========================================
.PHONY: build-drand
build-drand:
	@echo "Building drand for $(TARGET_ARCH)..."
	@echo "  Git tag: $(drand_tag)"
	$(BUILD_CMD) --build-arg=GIT_BRANCH=$(drand_tag) -t drand:latest -f drand/Dockerfile drand

.PHONY: build-lotus
build-lotus:
	@echo "Building lotus for $(TARGET_ARCH)..."
	@echo "  Git commit: $(lotus_tag)"
	$(BUILD_CMD) --build-arg=GIT_BRANCH=$(lotus_tag) -t lotus:latest -f lotus/Dockerfile lotus

.PHONY: build-forest
build-forest:
	@echo "Building forest for $(TARGET_ARCH)..."
	@echo "  Git commit: $(forest_commit)"
	$(BUILD_CMD) --build-arg GIT_COMMIT=$(forest_commit) -t forest:latest -f forest/Dockerfile forest

.PHONY: build-curio
build-curio:
	@echo "Building curio for $(TARGET_ARCH)..."
	@echo "  Git commit: $(curio_tag)"
	@echo "  Build mode: $(BUILD_MODE)"
	$(BUILD_CMD) --build-arg=GIT_BRANCH=$(curio_tag) --build-arg=LOTUS_TAG=$(lotus_tag) --build-arg=BUILD_MODE=$(BUILD_MODE) -t curio:latest -f curio/Dockerfile curio

.PHONY: build-workload
build-workload:
	@echo "Building workload for $(TARGET_ARCH)..."
	$(BUILD_CMD) -t workload:latest -f workload/Dockerfile workload

# ==========================================
# Compose commands
# ==========================================
.PHONY: up
up:
	cd config && $(builder) compose up -d

.PHONY: down
down:
	cd config && $(builder) compose down

.PHONY: logs
logs:
	cd config && $(builder) compose logs -f

.PHONY: restart
restart:
	cd config && $(builder) compose restart

# ==========================================
# Build groups
# ==========================================
.PHONY: build-infra
build-infra: build-drand
	@echo "Infrastructure images built."

.PHONY: build-nodes
build-nodes: build-lotus build-forest build-curio
	@echo "Node images built."

.PHONY: build-all
build-all: build-drand build-lotus build-forest build-curio build-workload
	@echo "All images built."

# ==========================================
# Full workflows
# ==========================================
.PHONY: all
all: build-all up
	@echo "All images built and localnet started."

.PHONY: rebuild
rebuild: down cleanup build-all up
	@echo "Clean rebuild complete."

.PHONY: cleanup
cleanup:
	cd config && $(builder) compose down 2>/dev/null || true
	./cleanup.sh

# ==========================================
# Help
# ==========================================
.PHONY: help
help:
	@echo "Filecoin Antithesis Makefile"
	@echo ""
	@echo "Build individual images:"
	@echo "  make build-drand      Build drand image"
	@echo "  make build-lotus      Build lotus image"
	@echo "  make build-forest     Build forest image"
	@echo "  make build-curio      Build curio image"
	@echo "  make build-workload   Build workload image"
	@echo ""
	@echo "Build groups:"
	@echo "  make build-infra      Build infrastructure (drand)"
	@echo "  make build-nodes      Build all node images (lotus, forest, curio)"
	@echo "  make build-all        Build all images"
	@echo ""
	@echo "Docker Compose:"
	@echo "  make up               Start localnet (docker compose up -d)"
	@echo "  make down             Stop localnet (docker compose down)"
	@echo "  make logs             Follow logs (docker compose logs -f)"
	@echo "  make restart          Restart all containers"
	@echo ""
	@echo "Workflows:"
	@echo "  make all              Build all images and start localnet"
	@echo "  make rebuild          Clean rebuild (down + cleanup + build + up)"
	@echo "  make cleanup          Stop containers and clean data"
	@echo ""
	@echo "Info:"
	@echo "  make show-versions    Show all image version tags"
	@echo "  make show-arch        Show target architecture"
	@echo ""
	@echo "Current arch: $(TARGET_ARCH)"