# This is used only for building the Antithesis configuration image
# See https://antithesis.com/docs/getting_started/setup.html#create-a-configuration-directory
FROM scratch

COPY ./docker-compose.yml /docker-compose.yml

COPY ./drand /drand
COPY ./lotus /lotus
COPY ./forest /forest
COPY ./.env /.env