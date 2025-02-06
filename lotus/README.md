## lotus
 
* Dockerfile to build lotus image
* `exclusion.txt` is used in the Dockerfile for instrumentation. It excludes certain diractories from being instrumented. It was put in place to prevent some errors.
* Startup scripts for lotus nodes.
* `config-*.toml` files are used in the `lotus-*.sh` startup scripts for configuration
* Patches for local drand and building lotus with data race checker
