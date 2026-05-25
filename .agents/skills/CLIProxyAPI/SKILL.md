```markdown
# CLIProxyAPI Development Patterns

> Auto-generated skill from repository analysis

## Overview
This skill teaches the core development patterns and conventions used in the CLIProxyAPI repository, a Go-based project for building command-line interface proxy APIs. It covers file organization, code style, commit conventions, and testing approaches, providing practical examples and step-by-step workflows to streamline contributions and maintenance.

## Coding Conventions

### File Naming
- Use **snake_case** for all file names.
  - Example: `cli_proxy_api.go`, `request_handler.go`

### Imports
- Use **relative imports** within the project.
  - Example:
    ```go
    import "./utils"
    ```

### Exports
- Use **named exports** for functions, types, and variables.
  - Example:
    ```go
    // In cli_proxy_api.go
    package main

    func StartProxy() {
        // implementation
    }
    ```

### Commit Messages
- Follow the **conventional commit** pattern.
- Use the `feat` prefix for new features.
- Keep commit messages concise (average ~23 characters).
  - Example:
    ```
    feat: add proxy handler
    ```

## Workflows

### Adding a New Feature
**Trigger:** When implementing a new feature or capability  
**Command:** `/add-feature`

1. Create a new file using snake_case (e.g., `feature_name.go`).
2. Implement the feature with named exports.
3. Use relative imports for any internal dependencies.
4. Write or update corresponding tests in a `*.test.*` file.
5. Commit your changes using the `feat` prefix:
    ```
    feat: short description of feature
    ```
6. Push your branch and open a pull request.

### Writing Tests
**Trigger:** When adding or updating functionality  
**Command:** `/write-test`

1. Create or update a test file matching the pattern `*.test.*` (e.g., `proxy_handler.test.go`).
2. Write tests for each exported function or method.
3. Use Go's standard testing package or the project's preferred approach.
4. Run tests to ensure correctness.

### Importing Internal Modules
**Trigger:** When reusing code from another part of the project  
**Command:** `/import-module`

1. Use a relative import path.
    ```go
    import "./utils"
    ```

## Testing Patterns

- Test files follow the `*.test.*` naming pattern (e.g., `handler.test.go`).
- The specific testing framework is unknown, but Go's standard `testing` package is likely.
- Each exported function should have corresponding tests.
- Example test file structure:
    ```go
    // handler.test.go
    package main

    import "testing"

    func TestStartProxy(t *testing.T) {
        // test implementation
    }
    ```

## Commands

| Command        | Purpose                                   |
|----------------|-------------------------------------------|
| /add-feature   | Scaffold and commit a new feature         |
| /write-test    | Create or update test files               |
| /import-module | Import internal modules using conventions  |
```
