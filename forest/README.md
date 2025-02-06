## forest
 
* Dockerfile to build forest image
* Startup scripts for forest nodes
    * `forest-init.sh` will create a forest node
    * `forest-connector.sh` will connect another forest node to the chain
* `forest_config.toml.tpl` is a configuration template that is used in `forest-init.sh`
* `forest.patch` is a patch to allow drand to run offline
