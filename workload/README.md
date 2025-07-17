# Filecoin Antithesis Workload

This directory contains the workload implementation for testing Filecoin nodes using the Antithesis platform. The workload is designed to test various aspects of Filecoin nodes including consensus, networking, state management, and smart contract interactions.

## Directory Structure

- `main.go`: Main entry point with CLI commands for different operations
- `main/`: Test Composer commands for generating test cases
- `resources/`: Helper functions and utilities
- `entrypoint/`: Entry point scripts for workload execution

## Writing New Workloads

There are several ways to add new workloads to test Filecoin nodes:

### 1. Adding CLI Commands (main.go)

The CLI uses `urfave/cli` for command structure. To add a new command:

1. Identify the appropriate command group (wallet, network, mempool, contracts, consensus, monitoring)

2. Add your command to the relevant group function:
```go
func someCommandGroup() *cli.Command {
    return &cli.Command{
        Name:  "group-name",
        Usage: "Group description",
        Subcommands: []*cli.Command{
            {
                Name:  "my-new-command",
                Usage: "Description of what the command does",
                Flags: []cli.Flag{
                    // Define command flags here
                },
                Action: func(c *cli.Context) error {
                    return performMyNewOperation(c.Context)
                },
            },
        },
    }
}
```

3. Implement the operation function with appropriate assertions:
```go
func performMyNewOperation(ctx context.Context) error {
    log.Printf("[INFO] Starting operation...")
    
    // Test your conditions using assertions
    assert.Sometimes(condition, "Expected behavior occurred", details)
    
    // Your operation logic here
    result := someOperation()
    
    // Assert the results
    assert.Always(result.isValid(), "Operation result should be valid", 
        map[string]interface{}{
            "result": result,
            "details": "Additional context about the assertion"
        })
    
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

// MyNewHelper performs a specific operation
// It returns an error if the operation fails
func MyNewHelper(ctx context.Context, api api.FullNode) error {
    log.Printf("[INFO] Starting helper operation...")
    // Helper implementation
    return nil
}
```

### 3. Writing Tests with Assertions

Tests in this workload use the Antithesis assertion framework to verify properties and behaviors. Here's how to write effective tests:

1. Define clear test properties:
```go
// Test that an operation completes successfully
assert.Sometimes(operationSucceeded, "Operation should succeed", 
    map[string]interface{}{
        "operation": "description",
        "impact": "What failure means for the system"
    })

// Test that invalid states never occur
assert.Always(isValidState, "System should maintain valid state",
    map[string]interface{}{
        "state": currentState,
        "requirement": "Why this state must be maintained"
    })
```

2. Use appropriate assertion types:
```go
// For properties that must always hold
assert.Always(condition, "Property description", details)

// For properties that must occur at least once
assert.Sometimes(condition, "Property description", details)

// For code paths that must be reached
assert.Reachable("Description of the reached state", details)

// For code paths that should never be reached
assert.Unreachable("Description of the unreachable state", details)
```

3. Provide detailed context in assertion details:
```go
details := map[string]interface{}{
    "component": "Which part of the system",
    "expected": expectedValue,
    "actual": actualValue,
    "impact": "Impact of failure",
    "requirement": "Why this assertion matters"
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

Example:
```bash
#!/bin/bash
# anytime_my_test.sh

# Use the CLI command you created
/opt/antithesis/app group-name my-new-command --flag value
```

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
2. **Logging**: Use structured logging with appropriate log levels (`[INFO]`, `[WARN]`, `[ERROR]`)
3. **Context**: Pass context.Context through the call chain for proper cancellation
4. **Configuration**: Use the config.json for configuration settings
5. **Assertions**: Use appropriate assertion types based on the property being tested
6. **Documentation**: Add comments explaining complex logic and test scenarios
7. **Resource Cleanup**: Ensure proper cleanup of resources in defer statements
8. **CLI Structure**: Follow the established command group organization
9. **Code Reuse**: Create helper functions in resources/ for shared functionality
10. **Test Properties**: Make assertions clear and provide detailed context in details map

## Example Workflow

Here's a typical workflow for adding a new test:

1. Identify the test scenario and which command group it belongs to
2. Add helper functions in `resources/` if needed
3. Add CLI command in `main.go` using the urfave/cli structure
4. Add appropriate assertions to verify test properties
5. Create Test Composer command in `main/`
6. Test and validate the implementation

