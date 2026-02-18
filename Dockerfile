FROM scratch

COPY docker-compose.yaml /docker-compose.yaml
COPY docker-compose2.yaml /docker-compose2.yaml

COPY ./.env /.env
COPY ../data /data
COPY ./drand /drand
COPY ./lotus /lotus
COPY ./forest /forest
COPY ./curio /curio
COPY ./yugabyte /yugabyte
COPY ./filwizard /filwizard