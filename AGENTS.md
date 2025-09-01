# Development Guide for Blocky

## Build & Test Commands
- `make build` - Build binary
- `make test` - Run all tests (excluding e2e)
- `make e2e-test` - Run e2e tests with Docker  
- `make race` - Run tests with race detector
- `make lint` - Run golangci-lint with gofmt/goimports
- `make fmt` - Format code with gofumpt and goimports
- `go test ./path/to/package` - Run single test package
- `go tool ginkgo -p --focus="TestName" ./path` - Run specific test with Ginkgo
- `go tool ginkgo --label-filter="!e2e" ./` - Run tests excluding e2e

## Code Style
- Use gofumpt for formatting (stricter than gofmt) 
- Import order: stdlib, 3rd party, local packages (grouped with blank lines)
- Use dot imports only for: ginkgo/gomega, config/migration, helpertest, model
- Error handling: wrap with fmt.Errorf("message: %w", err)
- Naming: camelCase for vars/funcs, PascalCase for exports
- Use receiver name abbreviations (e.g., `c *CachingResolver`)
- Prefer table-driven tests with Ginkgo/Gomega
- Mock interfaces with testify/mock or generate with go:generate
- No global variables except in main/cmd packages (use //nolint:gochecknoglobals)
- Use context.Context as first parameter in functions
- Package names should be lowercase, single word

## Testing
- Unit tests: `*_test.go` files using Ginkgo/Gomega
- E2E tests: separate `/e2e` directory with Docker integration  
- Test suites: `*_suite_test.go` files for Ginkgo setup
- Label e2e tests with `//go:build e2e` or Ginkgo labels