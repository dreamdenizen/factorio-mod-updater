# Factorio Mod Updater

A fast, compiled CLI tool for managing and updating mods on a Factorio dedicated server. Written in Go with zero runtime dependencies. Just drop the binary onto your server and run.

## Features

*   **One-command updates:** Fetches latest compatible releases from the [Factorio Mod Portal](https://mods.factorio.com) and downloads them automatically.
*   **Smart path inference:** Point it at your Factorio root directory and it finds the binary, mods folder, and config files for you.
*   **Dependency resolution:** Automatically discovers and installs any missing mod dependencies.
*   **Hash validation:** Every downloaded zip is verified against its SHA-1 signature.
*   **Space Age aware:** Built-in expansions (`space-age`, `quality`, `elevated-rails`) are automatically skipped.
*   **Good Looking Modern Terminal Output:** Spinners, colors, and progress bars galore
*   **Headless environment ready:** Automatically detects non-TTY environments like Pterodactyl, Pelican Panel, or CubeCoders AMP, disabling colors and spinners for clean log parsing.
*   **Fault tolerant:** Capable of partial updates. If a specific mod's metadata fails to resolve, progress continues for all other mods.
*   **Self-cleaning:** Automatically removes outdated version archives after successfully downloading new releases.

## Why Go?

This project exists because updating mods on a headless Factorio server with the wrong Python versions, Ruby gems, or other runtime dependencies is a pain. After encountering issues with Python and Ruby on various Linux distros, I wanted a single, self-contained binary that you can download and run immediately on any system, including Windows, and MacOS. Go makes that possible with static compilation and zero runtime dependencies.

## Installation

### Download a pre-built binary

Grab the latest release for your platform from the [Releases](../../releases) page.

On Linux/macOS, you will need to make the binary executable after downloading:

```bash
chmod +x mod_updater
```

You can then move it somewhere on your PATH if you would like to run it from anywhere:

```bash
sudo mv mod_updater /usr/local/bin/
```

## Usage

The simplest invocation uses your Factorio installation's root directory as a positional argument. By default, this will check for updates, display a status table, and automatically download upgrades if necessary.

```bash
# Check status and update all mods to their latest compatible release
./mod_updater ~/factorio

# Safe/Dry Run: Only list out-of-date mods without downloading updates
./mod_updater list ~/factorio
```

### Override Flags

All paths can be explicitly overridden if you're not using a standard installation layout:

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
./mod_updater --bin-path ~/factorio/bin/x64/factorio -m ~/factorio/mods -s ~/factorio/data/server-settings.json
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
│   ├── list.go                       # "list" subcommand with format negotiation
│   └── update.go                     # "update" subcommand with download pipeline
├── internal/factorio/
│   ├── updater.go                    # Core domain logic (API, downloads, hashing, deps)
│   └── updater_test.go               # Unit tests for version matching, parsing, hashing
├── .github/workflows/
│   ├── ci.yml                        # Runs tests on every push/PR
│   └── release.yml                   # Cross-compiles releases on version tags
├── .goreleaser.yml                   # GoReleaser cross-compilation config
├── go.mod
└── go.sum
```

## Build from source

If you have Go installed, you can build it yourself:

```bash
go build -o mod_updater
```

Cross-compile for other platforms:

```bash
GOOS=linux   GOARCH=amd64 go build -o build/mod_updater_linux_amd64
GOOS=windows GOARCH=amd64 go build -o build/mod_updater_windows_amd64.exe
GOOS=darwin  GOARCH=arm64 go build -o build/mod_updater_darwin_arm64
```

## Running Tests

The test suite includes over 50 unit and integration tests covering the core domain logic, path inference, and authentication fallback mechanisms.

```bash
go test -v -count=1 -race ./...
```

Tests requiring a live Factorio installation at `~/factorio` will automatically skip if the binary is not present (e.g. in CI environments).

## Acknowledgments

This project was heavily inspired by [pdemonaco/factorio-mod-updater](https://github.com/pdemonaco/factorio-mod-updater), a Python-based Factorio mod updater that I used for a long time until I ran into a host that I just couldn't get working with Python. This Go rewrite was built from the ground up with a focus on cross-platform support, dependency resolution, and a modern CLI experience.

## License

This project is licensed under the [MIT License](LICENSE).