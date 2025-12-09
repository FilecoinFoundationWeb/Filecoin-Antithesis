FROM scratch
COPY docker-compose.yml /docker-compose.yml
COPY docker-compose2.yml /docker-compose2.yml

COPY .env /.env
COPY ../data /data
COPY ./drand /drand
COPY ./lotus /lotus
COPY ./forest /forest
COPY ./curio /curio