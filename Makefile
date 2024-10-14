# drand + lotus are considered more stable dependencies
drand_tag = v2.0.4
lotus_tag = v1.29.1
runner = docker

# Todo: use buildx and specify the build platform x86
build-drand-docker:
	docker build --build-arg GIT_BRANCH=${drand_tag} --no-cache -t drand:${drand_tag} -f drand/Dockerfile drand

build-forest-docker:
	docker build --build-arg GIT_COMMIT=${forest_commit} --no-cache -t forest:${forest_commit} -f forest/Dockerfile forest

build-lotus-docker:
	docker build --build-arg=GIT_BRANCH=${lotus_tag} --no-cache -t lotus:${lotus_tag} -f lotus/Dockerfile lotus

# Build targets for podman
.PHONY: build-forest-podman
build-forest-podman:
	podman build --build-arg GIT_COMMIT=${forest_commit} -t forest:${forest_commit} -f forest/Dockerfile forest

.PHONY: build-drand-podman
build-drand-podman:
	podman build --build-arg=GIT_BRANCH=${drand_tag} -t drand:${drand_tag} -f drand/Dockerfile drand

.PHONY: build-lotus-podman
build-lotus-podman:
	podman build --build-arg=GIT_BRANCH=${lotus_tag} -t lotus:${lotus_tag} -f lotus/Dockerfile lotus

run-localnet:
		DRAND_TAG=${drand_tag} LOTUS_TAG=${lotus_tag} FOREST_COMMIT=${forest_commit} ${runner}-compose up

# Build everything and run local docker compose up
.PHONY: all
all: build-drand-${runner} build-forest-${runner} build-lotus-${runner} run-localnet

.PHONY: cleanup
cleanup:
	${runner}-compose down
	bash cleanup.sh
