# drand + lotus are considered more stable dependencies
drand_tag = v2.1.3
lotus_tag = v1.33.0
builder = docker
forest_commit = 631e7b1c68c4175aaffde4dd6641268d1854e646

.PHONY: build-forest
build-forest:
	${builder} build --build-arg GIT_COMMIT=${forest_commit} -t forest:${forest_commit} -f forest/Dockerfile forest

.PHONY: build-drand
build-drand:
	${builder} build --build-arg=GIT_BRANCH=${drand_tag} -t drand:${drand_tag} -f drand/Dockerfile drand

.PHONY: build-lotus
build-lotus:
	${builder} build --build-arg=GIT_BRANCH=${lotus_tag} -t lotus:${lotus_tag} -f lotus/Dockerfile lotus

.PHONY: run-localnet
run-localnet:
		DRAND_TAG=${drand_tag} LOTUS_TAG=${lotus_tag} FOREST_COMMIT=${forest_commit} ${builder}-compose up

# Build everything and run local docker compose up
.PHONY: all
all: build-drand build-forest build-lotus run-localnet

.PHONY: cleanup
cleanup:
	${builder}-compose down
	bash cleanup.sh
