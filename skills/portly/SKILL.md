---
name: portly
description: >-
  Find and manage development servers running on this machine using the `portly` CLI.
  Use when the user asks what's running on a port, wants to find a dev server, needs
  to free up or kill a port, identify a process's working directory, is debugging
  "port already in use" or "address already in use" errors, wants to see what
  an agent (Claude, Cursor, etc.) left running in the background, or needs a
  free port to start a server on. Use `portly register` to claim a port before
  starting a dev server. Triggers on "pick a port", "free port", "claim a port",
  "what's on port", "what's running on port", "find dev servers", "kill the
  process on port", "show process directory", "port already in use", "free up
  port", "check running servers", or "portly".
---

# portly — find and manage dev servers

`portly` reads the local socket table and works out which listening ports are
dev servers (as opposed to editors, chat apps, and OS daemons), so you don't
have to grep `lsof` output by hand.

Check `portly --version` or `which portly` first — if it's missing, install
with `brew install velvet-tiger/tap/portly` (macOS/Linux x86_64) or
`go install github.com/velvet-tiger/portly@latest`, and ask before installing
if the user hasn't already asked for it.

## Listing servers

```bash
portly
```

Shows only rows portly is confident are dev servers, as a table:

```
╭───────┬──────────────────┬────────────┬───────┬─────┬──────────────────────────────────────────────────────────╮
│ PORT  │ WHAT             │ PROJECT    │ PID   │ UP  │ WHY                                                      │
├───────┼──────────────────┼────────────┼───────┼─────┼──────────────────────────────────────────────────────────┤
│ 5174  │ node vite        │ storefront │ 28050 │ 11m │ launched by cursor (pid 12470)                           │
│ 5432  │ storefront-db-1  │ storefront │ 13546 │ 28m │ container storefront-db-1 in compose project storefront  │
╰───────┴──────────────────┴────────────┴───────┴─────┴──────────────────────────────────────────────────────────╯
2 shown, 40 hidden (38 application, 1 system, 1 unattributed). Use --all to see them.
```

The last line always prints — it names how many rows were hidden and why, so a
short list is never mistaken for "nothing found." If the port you're looking
for isn't in the default view, rerun with `--all`.

Flags:

| Flag | Effect |
|---|---|
| `--all` | Show every listening port, including applications and system daemons |
| `--json` | Emit JSON instead of a table (use this when parsing output programmatically) |
| `--probe` | Send a real HTTP GET to each port shown and report what it serves. Wakes idle servers and shows in their access logs, so it's off by default — mention this if suggesting it |
| `--probe-timeout` | How long each probe waits, default `400ms` |
| `--no-color` | Disable colour |
| `--version` | Print the version and exit |

## Showing a process directory

When a project name is too generic to identify the server, use the PID from the
listing to print its current working directory:

```bash
portly dir <pid>
```

This prints only the directory, making it useful for deciding which similarly
named project is running.

`--json` output shape (one object): `scanned_at`, `shown`, `hidden` (counts by
category), and `listeners[]` — each with `port`, `protocol`, `addresses`,
`relevance`, `reason`, `runtime`, `framework`, `agent`, `pid`, `parent_pid`,
`process`, `command_line`, `user`, `working_directory`, `project`.

## Killing a server

```bash
portly kill <port>
portly kill --force <port>   # SIGKILL instead of SIGTERM; flags come before the port
portly kill --project <name>
portly kill --force --project <name>
```

Finds the process listening on that port and signals it. A worker pool (e.g.
php-fpm master + workers sharing one port) is killed together.

`--project` signals every listening process whose `PROJECT` column equals the
given name, including all members of a worker pool. A process that exposes
multiple ports is signalled once. The name is a project directory basename, so
it can be ambiguous; inspect the PID with `portly dir <pid>` before using it.

If a matching port is published by a Docker container, portly refuses to
signal it — the process behind it is Docker's shared backend, so killing it
would affect every container, not just the selected project. It prints the
`docker stop <container>` command to run instead; run that command rather than
trying to kill the PID yourself.

**Killing a process is a real, hard-to-reverse action against the user's
running environment** — confirm the port/process with the user before running
`portly kill`, unless they already named the specific port or project to kill.

## Claiming a port before starting a server

When you start a dev server for the user, ask portly for a port first instead
of guessing one. That stops two servers fighting over 3000.

```bash
PORT=$(portly register --name web)   # run from inside the project
npm run dev -- --port "$PORT"
```

- stdout is only the port number. Errors go to stderr with exit code 1.
- The same `--name` in the same project always gets the same port back, so
  re-running is safe and the server keeps its URL across restarts.
- Pick a name per server in the project (`web`, `api`, `worker`).
- `portly register --name web 5174` claims an exact port. It fails if another
  project holds or claims it.
- Without a port, portly picks from `4000-4999`. Use `--range` for another.
- `--project <dir>` sets the project when you are not running from inside it.
- `--pid <pid>` ends the claim when that process exits.

Release a claim when the user no longer wants that server:

```bash
portly release --name web
portly release 4000
```

## Seeing free and used ports

```bash
portly ports                     # used ports in 4000-4999
portly ports --range 3000-3099
portly ports --free              # free ports, one per line
portly ports --json
```

States: `listening` (bound, unclaimed), `registered` (claimed, not bound),
`active` (claimed and held by its own project), `conflict` (claimed, but
another process holds it), `unavailable` (unclaimed, nothing visible, but the
port cannot be bound, usually another user's process). A `conflict` explains a
"port already in use" error before it happens.

## How classification works (for explaining a surprising result)

Rules run in confidence order, first match wins, and the `WHY` column names
which rule fired:

1. A container publishes this host port (`docker ps` supplies name/project).
2. A coding agent is in the parent process chain — strongest signal, beats everything below.
3. The process is a desktop app (executable lives in a `.app` bundle).
4. Installed software under `~/Library`, `~/.cursor`, etc. (version-manager interpreters like nvm are exempt — judged by the script they run).
5. The process runs from a system directory like `/usr/libexec`.
6. A dev runtime started from an editor, terminal, or shell.
7. A dev runtime whose working directory sits inside a project (nearest `.git`/`package.json`/`go.mod`/etc. wins in a monorepo).
8. A recognised framework in the command line.

Anything left over is `unattributed` — no rule explained it, but it may still
be a real server; check with `--all`.

## Known limits

- macOS and Linux only.
- Without `sudo`, ports held by another user or root are invisible — rerun under `sudo` if something expected is missing.
- If an agent backgrounds a server and its shell exits, the server reparents to `launchd` and rule 2 (agent attribution) no longer applies — it usually still surfaces via rule 7 (project directory), but `WHY` will name the project instead of the agent.
