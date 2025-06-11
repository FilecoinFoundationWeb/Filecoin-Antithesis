# Filecoin Antithesis Workload

This directory contains the workload implementation for testing Filecoin nodes using the Antithesis platform. The workload is designed to test various aspects of Filecoin nodes including consensus, networking, state management, and smart contract interactions.

## Directory Structure

- `main.go`: Main entry point with CLI commands for different operations
- `main/`: Test Composer commands for generating test cases
- `resources/`: Helper functions and utilities
- `go-test-scripts/`: Go test files for specific test scenarios
- `entrypoint/`: Entry point scripts for workload execution
- `types/`: Common type definitions

## Writing New Workloads

There are several ways to add new workloads to test Filecoin nodes:

### 1. Adding CLI Commands (main.go)

To add a new CLI command in `main.go`:

1. Add a new flag in `parseFlags()`:
```go
myNewFlag := flag.String("my-new-op", "", "Description of the new operation")
```

2. Update the operation list in validateInputs():
```go
validOps := map[string]bool{
    "my-new-op": true,
    // ... existing operations
}
```

3. Add a case in the main() switch statement:
```go
case "my-new-op":
    err = performMyNewOperation(ctx, nodeConfig)
```

4. Implement the operation function:
```go
func performMyNewOperation(ctx context.Context, nodeConfig *resources.NodeConfig) error {
    // Your operation logic here
    return nil
}
```

### 2. Adding Helper Functions (resources/)

The `resources/` directory contains reusable helper functions. To add new helpers:

1. Create a new file in `resources/` if it's a new category of functionality
2. Add your helper functions with proper documentation
3. Export functions that will be used by workloads

Example:
```go
// my_helper.go
package resources

func MyNewHelper(ctx context.Context, api api.FullNode) error {
    // Helper implementation
    return nil
}
```

### 3. Adding Go Tests (go-test-scripts/)

To add new test scenarios:

1. Create a new test file in `go-test-scripts/`
2. Use the Antithesis assertion framework for test properties
3. Follow the existing test patterns for consistency

Example:
```go
package main

import (
    "testing"
    "github.com/antithesishq/antithesis-sdk-go/assert"
)

func TestMyNewScenario(t *testing.T) {
    assert.Always(condition, "Test property description", details)
}
```

### 4. Adding Test Composer Commands (main/)

Test Composer commands in `main/` follow naming conventions:
- `parallel_*`: Commands that can run in parallel
- `anytime_*`: Commands that can run at any time
- `eventually_*`: Commands that verify eventual properties

To add a new command:

1. Create a new script following the naming convention
2. Use appropriate Test Composer annotations
3. Implement the test logic

## Using Antithesis Assertions

The workload uses the Antithesis Go SDK for defining test properties. Key assertion types:

- `assert.Always(condition, message, details)`: Property must hold every time
- `assert.Sometimes(condition, message, details)`: Property must hold at least once
- `assert.Reachable(message, details)`: Code must be reached
- `assert.Unreachable(message, details)`: Code must never be reached
- `assert.AlwaysOrUnreachable(condition, message, details)`: If reached, condition must hold

For detailed documentation on assertions, see:
[Antithesis Go Assert Documentation](https://antithesis.com/docs/generated/sdk/golang/assert/)

## Best Practices

1. **Error Handling**: Always return meaningful errors and use proper error wrapping
2. **Logging**: Use structured logging with appropriate log levels
3. **Context**: Pass context.Context through the call chain for proper cancellation
4. **Configuration**: Use the config.json for node-specific settings
5. **Assertions**: Use appropriate assertion types based on the property being tested
6. **Documentation**: Add comments explaining complex logic and test scenarios
7. **Resource Cleanup**: Ensure proper cleanup of resources in defer statements

## Example Workflow

Here's a typical workflow for adding a new test:

1. Identify the test scenario
2. Add helper functions in `resources/` if needed
3. Create test implementation in `go-test-scripts/`
4. Add CLI command in `main.go` if needed
5. Create Test Composer command in `main/`
6. Add appropriate assertions for test properties
7. Test and validate the implementation
```
