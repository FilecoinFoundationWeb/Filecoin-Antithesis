# drand + lotus are considered more stable dependencies
drand_tag = v2.0.4
lotus_tag = v1.29.1
builder = docker

.PHONY: build-forest
build-forest:
	${builder} build --build-arg GIT_COMMIT=${forest_commit} -t forest:${forest_commit} -f forest/Dockerfile forest

.PHONY: build-drand
build-drand:
	${builder} build --build-arg=GIT_BRANCH=${drand_tag} -t drand:${drand_tag} -f drand/Dockerfile drand

.PHONY: build-lotus
build-lotus:
	${builder} build --build-arg=GIT_BRANCH=${lotus_tag} -t lotus:${lotus_tag} -f lotus/Dockerfile lotus

run-localnet:
		DRAND_TAG=${drand_tag} LOTUS_TAG=${lotus_tag} FOREST_COMMIT=${forest_commit} ${builder}-compose up

# Build everything and run local docker compose up
.PHONY: all
all: build-drand build-forest build-lotus run-localnet

.PHONY: cleanup
cleanup:
	${builder}-compose down
	bash cleanup.sh
