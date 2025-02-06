## workload
 
* Dockerfile to build workload image

* `main.go` is the entrypoint of the application. It is a go binary with CLI flags for different operaions
* `main/` is a directory that contains test commands that are categorized by naming conventions (e.g., `parallel`, `anytime`, `eventually`). These are [Test Composer](https://antithesis.com/docs/test_templates/) commands that enable Antithesis to generate thousands of test cases that will run over a multitude of system states. Test Composer handles varying things like parallelism, test length, and command order.
    * Ex: `parallel_driver_spammer.py`, `anytime_node_height_progression.sh`, `eventually_all_node_sync_status_check.py`
* `resources/` container helper files like `rpc.py`, `wallets.py`, and smart contract files (`SimpleCoin.sol`)
* `removed/` contains deprecated or removed scripts. They are kept for reference / history.
* `go-test-scripts/` contains more go tests. These are called from an executable in `main/`.
