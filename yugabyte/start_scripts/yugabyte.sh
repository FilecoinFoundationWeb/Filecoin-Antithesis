#!/bin/bash

export YB_DATA_DIR=${YB_DATA_DIR}

#bin/yugabyted start --base_dir=${YB_DATA_DIR}

bin/yb-master --fs_data_dirs=/mnt/master --master_addresses=yugabyte:7100 --rpc_bind_addresses=yugabyte:7100 --replication_factor=1



sleep infinity