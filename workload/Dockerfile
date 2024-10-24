FROM docker.io/ubuntu:latest

RUN apt-get update -y && apt-get install -y python3 pip python3-requests python3-filelock curl jq

COPY ./lotus/first.sh /opt/antithesis/test/v1/lotus/first.sh
COPY ./lotus/parallel_driver_lotus_spammer.py /opt/antithesis/test/v1/lotus/parallel_driver_lotus_spammer.py
COPY ./lotus/parallel_driver_lotus_create_wallets.py /opt/antithesis/test/v1/lotus/parallel_driver_lotus_create_wallets.py
COPY ./lotus/parallel_driver_lotus_delete_wallets.py /opt/antithesis/test/v1/lotus/parallel_driver_lotus_delete_wallets.py

COPY ./forest/first.sh /opt/antithesis/test/v1/forest/first.sh
COPY ./forest/parallel_driver_forest_spammer.py /opt/antithesis/test/v1/forest/parallel_driver_forest_spammer.py
COPY ./forest/parallel_driver_forest_create_wallets.py /opt/antithesis/test/v1/forest/parallel_driver_forest_create_wallets.py
COPY ./forest/parallel_driver_forest_delete_wallets.py /opt/antithesis/test/v1/forest/parallel_driver_forest_delete_wallets.py

COPY ./resources/initialize_wallets.py /opt/antithesis/resources/initialize_wallets.py
COPY ./resources/rpc_url_and_auth_token.py /opt/antithesis/resources/rpc_url_and_auth_token.py
COPY ./resources/request.py /opt/antithesis/resources/request.py
COPY ./resources/rpc.py /opt/antithesis/resources/rpc.py
COPY ./resources/transaction.py /opt/antithesis/resources/transaction.py
COPY ./resources/wallets.py /opt/antithesis/resources/wallets.py
COPY ./resources/wallets.txt /opt/antithesis/resources/wallets.txt

ENTRYPOINT sleep infinity