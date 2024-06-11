FROM scratch

COPY ./docker-compose.yml /docker-compose.yml

COPY ./drand /drand
COPY ./lotus /lotus