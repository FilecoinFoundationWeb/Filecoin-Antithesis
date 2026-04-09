FROM scratch

COPY docker-compose.yaml /docker-compose.yaml

COPY ./.env /.env

# Profile-specific env files — Antithesis swaps /.env based on custom.setup
COPY ./env.nightly   /profiles/env.nightly
COPY ./env.consensus /profiles/env.consensus
COPY ./env.drand     /profiles/env.drand
COPY ./env.fip       /profiles/env.fip
COPY ./env.foc       /profiles/env.foc

COPY ./drand /drand
COPY ./lotus /lotus
COPY ./forest /forest
COPY ./curio /curio
COPY ./yugabyte /yugabyte
COPY ./filwizard /filwizard
COPY ./workload /workload