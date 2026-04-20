# Shared recipes for Go-based miner plugins

# lint project files
lint:
  golangci-lint run -c .golangci.yaml

# build plugin
build:
  go build -v ./...

# install plugin binary
install:
  go install -v .

# format project files
format:
  goimports -w .

# run go:generate directives
gen:
  go generate ./...

# clean build artifacts
clean:
  go clean ./...

# download and tidy dependencies
deps:
  go mod download
  go mod tidy

# run the plugin
run:
  go run .
