# Factorio Mod Updater

A fast and simple tool for updating mods on your Factorio dedicated server. You don't need to install Python, Ruby, or any other software. Just drop the file onto your server and run it!

![Factorio Mod Updater Demo](assets/update_demo.gif)

## Features

*   **One-command updates:** Connects to the [Factorio Mod Portal](https://mods.factorio.com) and automatically downloads the latest versions of your mods.
*   **Smart auto-detection:** Just point it at your Factorio folder and it finds everything it needs (mods, settings) all on its own.
*   **Handles dependencies:** If a mod needs another mod to work, the updater automatically finds and installs it for you.
*   **Safe downloads:** Checks every downloaded file to make sure it isn't corrupted, preventing broken `.zip` files from crashing your server.
*   **Space Age aware:** Built-in DLC expansions (`space-age`, `quality`, `elevated-rails`) are safely ignored.
*   **Beautiful terminal:** Enjoy a clean output with spinners, colors, and live progress bars as your mods download.
*   **Server panel friendly:** Works perfectly with server panels like Pterodactyl, Pelican Panel, or CubeCoders AMP. It automatically disables fancy colors and progress bars to keep your server logs clean and readable.
*   **Detailed log file:** Keeps a permanent record of everything it did (like what got updated or removed) in a handy `last-mod-update.log` file, just in case you need to check what happened.
*   **Bulletproof:** If one mod gets stuck or removed from the portal, the updater skips it and finishes the rest so your server can still start.
*   **Self-cleaning:** Automatically deletes old mod `.zip` files when a new version is downloaded, saving your server's disk space.

## Why use this tool?

Managing mods on a server can be a pain if you need to download a bunch of other stuff just to make a script work. This project exists to provide a single, ready-to-use program that you can download and run immediately on any system (Windows, Linux, or MacOS) without setting anything else up.

## Installation

### Download a pre-built binary

Grab the latest release for your platform from the [Releases](../../releases) page.

On Linux/macOS, you will need to make the file executable after downloading:

```bash
chmod +x mod_updater
```

You can then move it somewhere on your PATH if you would like to run it from anywhere without typing `./` first:

```bash
sudo mv mod_updater /usr/local/bin/
```

## Usage

The simplest way to use the updater is to just point it at your Factorio installation folder. By default, it will check for updates, show you what's old, and automatically download the upgrades.

```bash
# Check status and update all mods to their latest compatible release
./mod_updater ~/factorio

# Safe/Dry Run: Only list out-of-date mods without downloading updates
./mod_updater list ~/factorio
```

### Advanced: Override Flags

All paths can be explicitly overridden if you have a custom or unusual server setup:

| Flag | Short | Description |
|------|-------|-------------|
| `--bin-path` | `-b` | Path to your Factorio executable |
| `--mod-path` | `-m` | Path to your mods directory |
| `--server-settings` | `-s` | Path to your `server-settings.json` |
| `--player-data` | `-d` | Path to your `player-data.json` |
| `--username` | `-u` | Override factorio.com username |
| `--token` | `-t` | Override factorio.com API token |

```bash
# Example with explicit, custom paths
./mod_updater --bin-path ~/factorio/bin/x64/factorio -m ~/factorio/mods -s ~/factorio/data/server-settings.json
```

### Authentication

The updater needs to log in to the Mod Portal to download files. It looks for your Factorio account details (Username and Token) in this order:

1. CLI flags (`-u` and `-t` when you run the command)
2. Inside your `server-settings.json` file
3. Inside your `player-data.json` file

*(Note: Your Token is the unique code found on your factorio.com profile page, not your password!)*

---

## Technical Details (For Developers)

<details>
<summary>Click to view Project Structure and Build Instructions</summary>

### Project Structure

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

### Build from source

If you have Go 1.26+ installed, you can build the binary yourself:

```bash
go build -o mod_updater
```

Cross-compile for other platforms:

```bash
GOOS=linux   GOARCH=amd64 go build -o build/mod_updater_linux_amd64
GOOS=windows GOARCH=amd64 go build -o build/mod_updater_windows_amd64.exe
GOOS=darwin  GOARCH=arm64 go build -o build/mod_updater_darwin_arm64
```

### Running Tests

The test suite includes over 50 unit and integration tests covering the core logic.

```bash
go test -v -count=1 -race ./...
```
</details>

## Acknowledgments

This project was heavily inspired by [pdemonaco/factorio-mod-updater](https://github.com/pdemonaco/factorio-mod-updater), a script that I used for a long time until I ran into an OS that I just couldn't get working with Python. This tool was built from the ground up with a focus on ease-of-use, cross-platform support, and native terminal feedback.

## License

This project is licensed under the [MIT License](LICENSE).