# Port tracking

Status: implemented in `internal/registry` and `claims.go`.

## Goal

An app asks portly for a port before it binds. portly gives it one that nothing is using and nothing else has claimed. The claim is remembered, so two apps starting at the same time do not both pick 3000.

Today portly only observes the machine. Port tracking adds the first state portly keeps between runs.

## Commands

### `portly register`

Claims a port.

```bash
PORT=$(portly register --name web)            # pick a free port, print it
portly register --name web 5174               # claim this exact port
portly register --name api --range 8000-8099  # pick from a range
portly register --name web --pid 4242         # claim ends when pid 4242 exits
```

- A claim is keyed by project and name. The project comes from the current directory, using the same marker walk as `internal/classify/project.go`. `--project <dir>` overrides it.
- Running the command again with the same project and name returns the same port. The command is idempotent. Each app gets a stable port per project, so bookmarks, OAuth callback URLs and `.env` files stay valid.
- On success, stdout holds only the port number, so `$(...)` works. `--json` prints the full record.
- It exits 1 with a reason if the port is in use or claimed by another project and name.
- An exact port already held by the same project's server is allowed, so an app can register the port it is already running on.
- Asking for a different exact port under a name that already holds one is an error, not a move.
- The default allocation range is `4000-4999`. It avoids 3000, 5173, 8000 and 8080, which frameworks pick by default.

### `portly release`

Drops a claim.

```bash
portly release --name web    # this project's "web" claim
portly release 5174          # by port
```

`portly kill --project` does not release claims. Stopping a server and giving up its port are separate decisions.

### `portly ports`

Reports ports in a range. The range defaults to `4000-4999`. Listing every free port on the machine helps nobody.

```bash
portly ports                     # used ports in the default range
portly ports --range 3000-3099 --free
portly ports --json
```

Each used port has one state:

| State | Meaning |
|---|---|
| `listening` | Something is bound, with no claim |
| `registered` | Claimed, nothing bound yet |
| `active` | Claimed, and something is bound to it |
| `conflict` | Claimed by one project, but a process outside that project holds it |
| `unavailable` | Not claimed and no visible listener, but the test bind failed |

`conflict` catches a "port already in use" error before the app starts. A holder whose project is unknown counts as outside the project, so missing information never hides a conflict.

`--free` prints bare port numbers, one per line, for piping. `ports` never writes the registry. It leaves stale claims out of the report and counts them in the summary.

## Deciding a port is free

A port is free only if all three checks pass:

1. No claim in the registry.
2. No listener in the socket table, from the existing `scan.Scanner`.
3. A test bind succeeds on `127.0.0.1` and `::1`.

Check 3 exists because portly cannot see sockets held by other users without `sudo`. A test bind sees everything. It tries `0.0.0.0`, `::`, `127.0.0.1` and `::1`, with `SO_REUSEADDR` turned off. With reuse on, macOS lets a wildcard bind succeed beside a loopback listener. The cost is that a port with connections in `TIME_WAIT` reads as taken for a minute or so.

There is still a gap between portly closing the test socket and the app binding. The claim covers that gap for apps that use portly. An app that does not use portly can still take the port.

## Claim lifetime

A claim lasts until it is released or its project directory no longer exists. Stale claims are dropped the next time `register` or `release` writes the registry, and each one is named on stderr.

A claim is not tied to the caller's PID by default. In `PORT=$(portly register)`, portly's parent is a subshell that exits at once, so its PID says nothing about the app. `--pid <n>` ties a claim to a named process for apps that want that. Such a claim is dropped once that process has exited.

## Storage

- One JSON file at `$XDG_STATE_HOME/portly/ports.json`, falling back to `~/.local/state/portly/ports.json`. macOS uses the same path.
- Writers hold an exclusive `flock` on a sibling lock file for the whole load, decide and save cycle. They write a temp file and `rename` it over the old one, so a crash never leaves a partial file. `flock` exists on both supported platforms.
- No daemon. portly stays a single binary you run and forget.

A record:

```json
{
  "port": 5174,
  "project": "mockups",
  "project_dir": "/Users/me/code/mockups",
  "name": "web",
  "registered_at": "2026-10-23T14:30:00Z",
  "pid": null
}
```

## Code layout

New package `internal/registry`, in the style of `internal/classify`:

- `Store` interface with `Load` and `Save`. A file-backed implementation handles locking and atomic writes. Tests use an in-memory store.
- `BindChecker` interface, so allocation tests do not open real sockets.
- `Allocate(claims, listening, checker, request) (Claim, error)` is a pure decision function. It reads and writes nothing.
- `Statuses(claims, listeners, portRange) []PortStatus` builds the `ports` output.

`claims.go` in package `main` holds `mainRegister`, `mainRelease` and `mainPorts`, built like `mainKill`. Each loads the registry under the lock, decides, then saves. A malformed registry file is an error. It is never reset, because a reset would hand claimed ports to other apps.

## Later

- Show the claim in the default table's `WHY` column, for example `registered as web in mockups`.
