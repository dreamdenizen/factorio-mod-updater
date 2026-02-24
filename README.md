# Factorio Mod Updater

A fast, compiled CLI tool for managing and updating mods on a Factorio dedicated server. Written in Go with zero runtime dependencies. Just drop the binary onto your server and run.

## Features

- **One-command updates:** Fetches latest compatible releases from the [Factorio Mod Portal](https://mods.factorio.com) and downloads them automatically.
- **Smart path inference:** Point it at your Factorio root directory; it finds the binary, mods folder, and config files for you.
- **Dependency resolution:** Automatically discovers and installs missing transitive mod dependencies.
- **Hash validation:** Every downloaded zip is verified against its SHA-1 signature.
- **Colorized output:** Up-to-date mods render green, outdated mods render red.
- **Space Age aware:** Built-in expansions (`space-age`, `quality`, `elevated-rails`) are automatically skipped.

## Installation

Download the latest binary for your platform from the [Releases](../../releases) page, or build from source:

```bash
go build -o mod_updater
```

Cross-compile for other platforms:

```bash
GOOS=linux   GOARCH=amd64 go build -o build/mod_updater_linux_amd64
GOOS=windows GOARCH=amd64 go build -o build/mod_updater_windows_amd64.exe
GOOS=darwin  GOARCH=arm64 go build -o build/mod_updater_darwin_arm64
```

## Usage

The simplest invocation uses your Factorio installation's root directory as a positional argument:

```bash
# List all mods and their update status
./mod_updater list ~/factorio

# Update all mods to their latest compatible release
./mod_updater update ~/factorio
```

### Override Flags

All paths can be explicitly overridden if your installation layout is non-standard:

| Flag | Short | Description |
|------|-------|-------------|
| `--bin-path` | `-b` | Path to the Factorio executable |
| `--mod-path` | `-m` | Path to the mods directory |
| `--server-settings` | `-s` | Path to `server-settings.json` |
| `--player-data` | `-d` | Path to `player-data.json` |
| `--username` | `-u` | Override factorio.com username |
| `--token` | `-t` | Override factorio.com API token |

```bash
# Example with explicit paths
./mod_updater update --bin-path ~/factorio/bin/x64/factorio -m ~/factorio/mods -s ~/factorio/data/server-settings.json
```

### Authentication

Credentials are resolved in this order:

1. CLI flags (`-u` / `-t`)
2. `server-settings.json` (`username` / `token` fields)
3. `player-data.json` (`service-username` / `service-token` fields)

## Project Structure

```
.
├── main.go                           # Entrypoint
├── cmd/
│   ├── root.go                       # Cobra root command, flag definitions, path inference
│   ├── root_test.go                  # Unit tests for path inference logic
│   ├── list.go                       # "list" subcommand with colorized table output
│   └── update.go                     # "update" subcommand with download pipeline
├── internal/factorio/
│   ├── updater.go                    # Core domain logic (API, downloads, hashing, deps)
│   └── updater_test.go              # Unit tests for version matching, parsing, hashing
├── .github/workflows/
│   ├── ci.yml                        # Runs tests on every push/PR
│   └── release.yml                   # Cross-compiles releases on version tags
├── .goreleaser.yml                   # GoReleaser cross-compilation config
├── go.mod
└── go.sum
```

## Running Tests

```bash
go test ./...
```

## Acknowledgments

This project was inspired by [pdemonaco/factorio-mod-updater](https://github.com/pdemonaco/factorio-mod-updater), a Python-based Factorio mod updater. This Go rewrite was built from the ground up with a focus on cross-platform support, dependency resolution, and a modern CLI experience.

## License

This project is licensed under the [MIT License](LICENSE).
