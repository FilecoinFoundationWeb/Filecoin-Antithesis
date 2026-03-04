FROM scratch

COPY docker-compose.yaml /docker-compose.yaml
COPY docker-compose.override.yaml* /

COPY ./.env /.env
COPY ./test.env /test.env
COPY ./drand /drand
COPY ./lotus /lotus
COPY ./forest /forest
COPY ./curio /curio
COPY ./yugabyte /yugabyte
COPY ./filwizard /filwizard
COPY ./workload /workload