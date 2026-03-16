# Skill: Issue Handler CLI (Go)

The `issue-handler` is a Go-based autonomous service that polls Gitea issues and executes an agent-based workflow to resolve them.

## Project Structure

- `cmd/issue-handler/main.go`: Minimal entry point, handles CLI flags and starts the loop.
- `pkg/system/config.go`: Handles configuration loading using reflection-based environment variable mapping.
- `pkg/fabric/issue_handler.go`: Contains the core logic for Gitea/Telegram interactions and issue processing.
- `pkg/file/file.go`: Utility functions for file system operations, including root path detection.

## Usage

### Prerequisites

- Go 1.22+
- Gitea instance
- `tea` CLI (preferred default transport for Gitea operations)
- Docker (optional, if `tea` is run via container)

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GITEA_BOT_BASE_URL` | `http://localhost:3000` | Gitea API base URL |
| `GITEA_BOT_TOKEN` | (required) | Gitea API token |
| `GITEA_BOT_OWNER` | `eslider` | Repository owner |
| `GITEA_BOT_REPO` | `ai-fabric` | Repository name |
| `GITEA_CLI_ENABLED` | `1` | Use `tea` CLI for API requests (recommended default) |
| `ISSUE_BASE_BRANCH` | `main` | Base branch for worktrees |
| `ISSUE_POLL_INTERVAL_SEC` | `45` | Polling interval |
| `ISSUE_HANDLER_DRY_RUN` | `0` | If `1`, do not perform actual changes |

### Command Line Flags

- `--once`: Run a single polling cycle and exit.
- `--issue-number <int>`: Process only a specific issue number.

## Testing Strategy (TDD)

The project follows a TDD approach for core logic. Tests are located in `cmd/issue-handler/main_test.go`.

### Running Tests

```bash
go test -v ./cmd/issue-handler/...
```

### Key Test Areas

1.  **Full Configuration Loading**: Ensures the `Config` struct is correctly initialized from environment variables using `mapstructure` and a generic underscore-based recursive map builder.
2.  **Issue Classification**: Ensures issues are correctly categorized as `bug` or `feature` based on keywords in title and body.
3.  **Skill Selection**: Validates that the correct documentation and skill files are associated with an issue based on its content.

## Workflow

1.  **Polling**: The handler lists open issues from Gitea.
2.  **Classification**: Each issue is classified to determine the workflow (bug vs feature).
3.  **Skill Selection**: Relevant skills are selected to guide the agent.
4.  **Worktree Management**: A Git worktree is prepared for the issue branch.
5.  **Agent Execution**: An autonomous agent is invoked with a generated prompt.
6.  **Verification**: Automated tests and linting are run.
7.  **Submission**: A PR is created with the proposed changes.

## Laconic Code Style

When working with `mapstructure` and nested configurations, keep the code laconic. Avoid redundant struct tags when the field name already matches the expected key (after splitting environment variables by underscore).

### Example

**Bad:**
```go
type GiteaCLIConfig struct {
	Bin    string `mapstructure:"BIN"`
	Image  string `mapstructure:"IMAGE"`
}
```

**Good:**
```go
type GiteaCLIConfig struct {
	Bin    string
	Image  string
}
```

Only use `mapstructure` tags when the environment variable name differs significantly from the field name (e.g., `BaseURL string ` + "`" + `mapstructure:"BOT_BASE_URL"` + "`" + `).
