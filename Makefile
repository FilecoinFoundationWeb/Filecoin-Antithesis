drand_tag = v2.0.4

build-drand:
	docker build --build-arg GIT_BRANCH=${drand_tag} --no-cache -t drand:${drand_tag} -f drand/Dockerfile drand
