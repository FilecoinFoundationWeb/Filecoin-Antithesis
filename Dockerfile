FROM scratch

COPY docker-compose.yaml /docker-compose.yaml

COPY ./.env /.env

# Profile-specific env files — Antithesis copies env.<custom.setup> to .env
COPY ./env.nightly   /env.nightly
COPY ./env.consensus /env.consensus
COPY ./env.drand     /env.drand
COPY ./env.fip       /env.fip
COPY ./env.foc       /env.foc

COPY ./drand /drand
COPY ./lotus /lotus
COPY ./forest /forest
COPY ./curio /curio
COPY ./yugabyte /yugabyte
COPY ./filwizard /filwizard
COPY ./workload /workload