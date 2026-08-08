# portly

Finds the development servers running on your machine, including the ones an agent started and forgot about.

Most listening ports on a developer machine are not dev servers. Editors, chat apps and OS daemons hold ports open too. portly reads the socket table, works out which process is behind each port, and shows only the dev servers.

## Install

On macOS, Homebrew needs nothing else installed:

```bash
brew install velvet-tiger/tap/portly
```

On Linux, download a binary from the [releases page](https://github.com/velvet-tiger/portly/releases), or use Go.

With Go, which puts the binary in `$(go env GOPATH)/bin`:

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

portly reads the socket table, then looks at the process behind each port: its command line, its working directory, what launched it, and whether a container publishes the port. Rules are applied in confidence order and the first match wins. The `WHY` column names the rule that fired. A wrong result points you at the rule to fix.

1. A container publishes this host port. `docker ps` supplies the name and compose project.
2. A coding agent is in the parent chain. This is the strongest signal and it beats everything below it.
3. The process is a desktop application, meaning its executable lives in a `.app` bundle.
4. The process is installed software under `~/Library`, `~/.cursor` and similar. An interpreter from a version manager such as nvm is exempt: it is judged by the script it runs, not by where it is installed.
5. The process runs from a system directory such as `/usr/libexec`.
6. A dev runtime started from an editor, terminal or shell.
7. A dev runtime whose working directory sits inside a project.
8. A recognised framework in the command line.

Anything left over is reported as unattributed, which means no rule explained it. It may still be a real server.

Rule 3 sits above rules 6 and 7 so that an editor with a project directory open is reported as an editor, not as that project's dev server.

Projects are found by walking up from the working directory looking for a marker such as `.git`, `package.json`, `composer.json`, `go.mod` or `pyproject.toml`. The nearest marker wins, so a package in a monorepo resolves to the package rather than the repository root.

## What portly does not do

**It runs on macOS and Linux only.** On any other platform portly exits with an error naming the platform rather than reporting zero ports.

**It only sees your own processes.** Without `sudo`, a port held by another user or by root does not appear. Run it under `sudo` if you suspect something is missing.

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

`.github/workflows/release.yml` runs GoReleaser on a macOS runner. The runner matters: the release runs the test suite first, and only a macOS runner exercises the Darwin side of the socket reader.

The build matrix is darwin and linux, each for arm64 and amd64. portly compiles for Windows, but that binary exits with an error because no Windows reader exists, so it is not shipped.

To verify a release without publishing:

```bash
goreleaser release --snapshot --clean --skip=publish
```
