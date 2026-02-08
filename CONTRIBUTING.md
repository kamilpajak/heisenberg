# Contributing to Heisenberg

Thank you for your interest in contributing to Heisenberg!

## Development Setup

### Prerequisites

- Go 1.24 or later
- [golangci-lint](https://golangci-lint.run/welcome/install/)
- [Lefthook](https://github.com/evilmartians/lefthook) (optional, for git hooks)

### Getting Started

1. Fork and clone the repository:

```bash
git clone https://github.com/YOUR_USERNAME/heisenberg.git
cd heisenberg
```

2. Install dependencies:

```bash
go mod download
```

3. Set up git hooks (optional but recommended):

```bash
lefthook install
```

4. Build and test:

```bash
make build
make test
```

## Code Style

- Follow standard Go conventions and idioms
- Run `gofmt` before committing (enforced by pre-commit hook)
- Run `golangci-lint run` to check for issues

## Commit Messages

We use [Conventional Commits](https://www.conventionalcommits.org/):

```
type(scope): description

[optional body]
```

Types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`

Examples:
- `feat(cli): add --output flag for JSON output`
- `fix(github): handle rate limiting correctly`
- `docs: update installation instructions`

## Pull Request Process

1. Create a feature branch from `main`:

```bash
git checkout -b feature/your-feature
```

2. Make your changes with clear, focused commits

3. Ensure all tests pass:

```bash
make test
make lint
```

4. Push and create a pull request

5. Wait for CI checks and code review

## Testing

- Write tests for new functionality
- Use table-driven tests where appropriate
- Run the full test suite before submitting:

```bash
go test -race ./...
```

## Reporting Issues

When reporting bugs, please include:

- Go version (`go version`)
- Operating system
- Steps to reproduce
- Expected vs actual behavior
- Any relevant logs or error messages

## Feature Requests

Feature requests are welcome! Please open an issue describing:

- The problem you're trying to solve
- Your proposed solution
- Any alternatives you've considered

## Code of Conduct

Be respectful and constructive in all interactions. We're all here to build something useful together.

## License

By contributing, you agree that your contributions will be licensed under the Apache 2.0 License.
