# portly

Finds the development servers running on your machine, including the ones an agent started and forgot about.

A developer machine holds far more listening ports than servers. The machine this was built against had 52 listening TCP sockets. Five were dev servers. The rest were PyCharm, WebStorm, Discord, Control Center and a handful of OS daemons. Reading the socket table is easy. Telling those two groups apart is the work, and that is what portly does.

## Install

Homebrew, which needs nothing else installed:

```bash
brew install velvet-tiger/tap/portly
```

Or with Go, which puts the binary in `$(go env GOPATH)/bin`:

```bash
go install github.com/velvet-tiger/portly@latest
```

That directory is not on `PATH` by default. If `portly` is not found afterwards:

```bash
echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.zshrc
```

From a clone:

```bash
go build -o portly . && ./portly
```

Output:

```
╭───────┬──────────────────┬────────────┬───────┬─────┬──────────────────────────────────────────────────────────╮
│ PORT  │ WHAT             │ PROJECT    │ PID   │ UP  │ WHY                                                      │
├───────┼──────────────────┼────────────┼───────┼─────┼──────────────────────────────────────────────────────────┤
│ 5174  │ node vite        │ mockups    │ 28050 │ 11m │ launched by cursor (pid 12470)                           │
│ 5432  │ consultmed-db-1  │ consultmed │ 13546 │ 28m │ container consultmed-db-1 in compose project consultmed  │
│ 8000  │ consultmed-api-1 │ consultmed │ 13546 │ 28m │ container consultmed-api-1 in compose project consultmed │
│ 8080  │ consultmed-web-1 │ consultmed │ 13546 │ 28m │ container consultmed-web-1 in compose project consultmed │
│ 52349 │ kelpie-crm-db-1  │ kelpie-crm │ 13546 │ 28m │ container kelpie-crm-db-1 in compose project kelpie-crm  │
╰───────┴──────────────────┴────────────┴───────┴─────┴──────────────────────────────────────────────────────────╯
5 shown, 40 hidden (38 application, 1 system, 1 unattributed). Use --all to see them.
```

The last line is always printed. portly never drops rows without saying how many and why.

## Flags

| Flag | Effect |
|---|---|
| `--all` | Show every listening port, including applications and system daemons |
| `--json` | Emit JSON instead of a table |
| `--probe` | Send an HTTP GET to each port shown and report what it serves |
| `--probe-timeout` | How long each probe waits, default `400ms` |
| `--no-color` | Disable colour. `NO_COLOR` and a non-terminal stdout also disable it |
| `--version` | Print the version and exit |

## How a port gets classified

portly reads the socket table, then asks four questions about the process behind each port: what is its command line, what is its working directory, what launched it, and is a container publishing this port. Rules are applied in confidence order and the first match wins. The `WHY` column names the rule that fired, so you can argue with a result instead of just distrusting it.

1. A container publishes this host port. `docker ps` supplies the name and compose project.
2. A coding agent is in the parent chain. This is the strongest signal and it beats everything below it.
3. The process is a desktop application, meaning its executable lives in a `.app` bundle.
4. The process is installed software under `~/Library`, `~/.cursor`, `~/.nvm` and similar.
5. The process runs from a system directory such as `/usr/libexec`.
6. A dev runtime started from an editor, terminal or shell.
7. A dev runtime whose working directory sits inside a project.
8. A recognised framework in the command line.

Anything left over is reported as unattributed. That is not a claim it is noise, only that no rule explained it.

Rule order matters. Rule 3 sits above rules 6 and 7 so that an editor with a project directory open is still reported as an editor, not as that project's dev server.

Projects are found by walking up from the working directory looking for a marker such as `.git`, `package.json`, `composer.json`, `go.mod` or `pyproject.toml`. The nearest marker wins, so a package in a monorepo resolves to the package rather than the repository root.

## What portly does not do

**It only runs on macOS.** The socket reader is behind an interface and the process layer is already cross-platform, so Linux is a small addition. It has not been written or run, so it is not offered. On any other platform portly exits with an error naming the platform rather than reporting zero ports.

**Windows needs more than a socket reader.** Most Windows developers run node, python and php inside WSL2. Those servers appear on the Windows host behind a relay process with no attributable PID and no working directory. Docker has `docker ps` to join against. WSL has no equivalent, so useful Windows support means running the Linux reader inside each distro through `wsl.exe`. That is a separate piece of work, not a flag.

**It only sees your own processes.** Without `sudo`, the kernel reports sockets owned by the calling user. A port held by another user or by root is absent, and the OS reports that absence as an empty result rather than as an error. portly cannot tell the difference and does not pretend to. Run it under `sudo` if you suspect something is missing.

**Agent attribution breaks when a server is orphaned.** Rule 2 walks the parent chain. If an agent backgrounds a server and the shell then exits, the server is reparented to `launchd` and the chain to the agent is gone. Such a server is usually still found by its working directory under rule 7, but the `WHY` column will name the project rather than the agent.

**Probing sends real traffic.** `--probe` opens a connection to each port shown and sends a GET. That can wake an idle dev server and will appear in its access log. It is off by default for that reason. The probe uses the port's actual bind address, so a server bound only to `::1` is reached correctly.

## Development

```bash
go test ./...
go vet ./...
```

Every rule that decides what a port is lives in `internal/classify` and is a pure function over injected inputs. The filesystem arrives as a `DirectoryReader` and the home directory as a string, so classification is tested against a fabricated tree with no running processes involved.

## Releasing

Pushing a tag builds and publishes. Nothing releases on an ordinary push.

```bash
git tag v0.1.0 && git push origin v0.1.0
```

`.github/workflows/release.yml` runs GoReleaser on a macOS runner. macOS matters: the release runs the test suite first, and on Linux the build-tagged stub would compile instead of the Darwin socket reader, so the suite would not test what ships.

The build matrix is `darwin/arm64` and `darwin/amd64` only. portly compiles for Linux and Windows, but those binaries exit with an error because only the Darwin reader exists. Shipping them would hand people a download that fails on first run. When a Linux reader lands, add it to `.goreleaser.yaml` in the same change.

To verify a release without publishing:

```bash
goreleaser release --snapshot --clean --skip=publish
```
