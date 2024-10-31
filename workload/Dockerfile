FROM docker.io/ubuntu:latest

RUN apt-get update -y && apt-get install -y python3 pip python3-requests python3-filelock curl jq

#good
COPY ./main/first.sh /opt/antithesis/test/v1/main/first.sh
COPY ./main/parallel_driver_spammer.py /opt/antithesis/test/v1/main/parallel_driver_spammer.py
COPY ./main/parallel_driver_create_wallets.py /opt/antithesis/test/v1/main/parallel_driver_create_wallets.py
COPY ./main/parallel_driver_delete_wallets.py /opt/antithesis/test/v1/main/parallel_driver_delete_wallets.py
COPY ./main/anytime_increasing_block_height_check.py /opt/antithesis/test/v1/main/anytime_increasing_block_height_check.py
COPY ./main/eventually_all_node_sync_status_check.py /opt/antithesis/test/v1/main/eventually_all_node_sync_status_check.py

#in progress
#COPY ./main/anytime_node_sync_check.py /opt/antithesis/test/v1/main/anytime_node_sync_check.py


COPY ./resources/initialize_wallets.py /opt/antithesis/resources/initialize_wallets.py
COPY ./resources/nodes.py /opt/antithesis/resources/nodes.py
COPY ./resources/request.py /opt/antithesis/resources/request.py
COPY ./resources/rpc.py /opt/antithesis/resources/rpc.py
COPY ./resources/transaction.py /opt/antithesis/resources/transaction.py
COPY ./resources/wallets.py /opt/antithesis/resources/wallets.py
COPY ./resources/wallets.txt /opt/antithesis/resources/wallets.txt


COPY ./sdk/antithesis_sdk.py /opt/antithesis/sdk/antithesis_sdk.py


ENTRYPOINT sleep infinity
