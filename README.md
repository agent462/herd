# Herd

A single-binary CLI tool for running commands across multiple SSH hosts simultaneously. Herd executes commands in parallel, groups identical output together, and shows unified diffs for hosts that differ.

## Why Herd

Tools like `pssh`, `pdsh`, and `ansible` can run commands across hosts, but none of them group identical output or show diffs between hosts. Herd treats identical output as the norm and surfaces outliers, so you can instantly see which hosts match and which differ.

## Install

```bash
go install github.com/agent462/herd/cmd/herd@latest
```

Or build from source:

```bash
git clone https://github.com/agent462/herd.git
cd herd
go build -o herd ./cmd/herd/
```

## Quick Start

No config file required. Pass hosts directly on the command line:

```bash
herd exec "cat /etc/os-release | grep PRETTY" pi-garage pi-livingroom pi-workshop
```

Output:

```
 2 hosts identical:
   pi-garage, pi-livingroom
   PRETTY_NAME="Debian GNU/Linux 12 (bookworm)"

 1 host differs:
   pi-workshop
   PRETTY_NAME="Debian GNU/Linux 11 (bullseye)"

3 succeeded
```

One host is running a different OS version. You can see exactly what differs at a glance.

## Usage

```
herd exec [command] [hosts...] [flags]
```

### Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--group` | `-g` | Use a host group from config |
| `--concurrency` | | Max parallel connections (default 20) |
| `--timeout` | | Per-host timeout, e.g. `30s`, `1m` (default 30s) |
| `--json` | | Output results as JSON |
| `--errors-only` | | Only show failed hosts |
| `--insecure` | | Skip host key verification |

### Examples

```bash
# Check which kernel version each host is running
herd exec "uname -r" -g pis

# Verify a service is running across your web tier
herd exec "systemctl is-active nginx" -g web --errors-only

# See disk usage, grouped by identical output
herd exec "df -h / | tail -1" -g pis

# JSON output for scripting
herd exec "cat /etc/hostname" -g pis --json

# Custom timeout and concurrency
herd exec "apt list --upgradable 2>/dev/null | wc -l" -g all --timeout 60s --concurrency 10
```

### Grouped Output with Diffs

When hosts return different output, herd shows a unified diff against the majority:

```
 2 hosts identical:
   pi-garage, pi-livingroom
   PRETTY_NAME="Debian GNU/Linux 12 (bookworm)"

 1 host differs:
   pi-workshop
   PRETTY_NAME="Debian GNU/Linux 11 (bullseye)"

   --- norm
   +++ outlier
   -PRETTY_NAME="Debian GNU/Linux 12 (bookworm)"
   +PRETTY_NAME="Debian GNU/Linux 11 (bullseye)"

3 succeeded
```

### JSON Output

```bash
herd exec "hostname" -g pis --json
```

```json
[
  {
    "host": "pi-garage",
    "stdout": "pi-garage\n",
    "stderr": "",
    "exit_code": 0,
    "duration": "52ms"
  }
]
```

## Configuration

Herd reads `~/.config/herd/config.yaml` if it exists. You can define host groups and default settings:

```yaml
groups:
  pis:
    hosts:
      - pi-garage
      - pi-livingroom
      - pi-workshop
      - pi-backyard
  web:
    hosts:
      - web-01
      - web-02
      - web-03
    user: deploy
    timeout: 10s

defaults:
  concurrency: 20
  timeout: 30s
  output: grouped
```

Groups support per-group `user` and `timeout` overrides.

### SSH Config

Herd reads `~/.ssh/config` and resolves `Host`, `User`, `Port`, `IdentityFile`, and `ProxyJump` for each host. Hosts not defined in the herd config will still work if they are in your SSH config.

### Authentication

Herd tries authentication methods in this order:

1. SSH agent (via `SSH_AUTH_SOCK`)
2. Key files (from `~/.ssh/config` IdentityFile or default locations)
3. Password prompt (interactive terminal only)

## Exit Codes

- `0` if all hosts succeed
- `1` if any host fails or returns a non-zero exit code

## Architecture

```
internal/
  config/       Config file parsing, host group resolution, SSH config merging
  ssh/          SSH client, auth chain, host key verification
  executor/     Parallel command execution with bounded concurrency
  grouper/      Output hashing, grouping by identical output, unified diffing
  ui/exec/      Terminal output formatting (grouped, JSON, errors-only)
```

## License

MIT
