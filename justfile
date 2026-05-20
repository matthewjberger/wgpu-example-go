set windows-shell := ["powershell.exe"]

# Displays the list of available commands
@just:
    just --list

# Runs the desktop app
run:
    go run ./cmd/wgpu-example-go

# Builds the desktop binary
build:
    go build ./cmd/wgpu-example-go

# Builds the wasm binary into site/ (Windows)
[windows]
build-wasm:
    $env:GOOS = "js"; $env:GOARCH = "wasm"; go build -o site/main.wasm ./cmd/wgpu-example-go

# Builds the wasm binary into site/ (Unix)
[unix]
build-wasm:
    GOOS=js GOARCH=wasm go build -o site/main.wasm ./cmd/wgpu-example-go

# Serves site/ on http://localhost:8080
serve:
    go run ./cmd/serve

# Builds the wasm binary and serves site/
run-wasm: build-wasm serve

# Runs go vet and fails on unformatted files (Windows)
[windows]
check:
    go vet ./...
    $unformatted = (gofmt -l . | Out-String).Trim(); if ($unformatted) { Write-Host $unformatted; exit 1 }

# Runs go vet and fails on unformatted files (Unix)
[unix]
check:
    go vet ./...
    unformatted=$(gofmt -l .); if [ -n "$unformatted" ]; then echo "$unformatted"; exit 1; fi

# Formats all Go files
format:
    gofmt -w .

# Runs all tests
test:
    go test ./...

# Runs check + test (use this before pushing)
ci: check test

# Lists all module dependencies with available updates
outdated:
    go list -m -u all

# Shows what `go mod tidy` would change without applying it
tidy-check:
    go mod tidy -diff

# Tidies go.mod / go.sum
tidy:
    go mod tidy

# Runs every read-only check: vet+fmt, tidy diff, outdated, tests
audit: check tidy-check outdated test

# Renders package doc for ./render
doc:
    go doc -all ./render

# Removes the desktop binary (Windows)
[windows]
clean:
    Remove-Item -Force -ErrorAction SilentlyContinue wgpu-example-go.exe

# Removes the desktop binary (Unix)
[unix]
clean:
    rm -f wgpu-example-go

# Displays Go tool version
@versions:
    go version
