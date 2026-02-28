# Herd

A single-binary CLI tool for running commands across multiple SSH hosts simultaneously. Herd executes commands in parallel, groups identical output together, and shows unified diffs for hosts that differ. Includes recipes for multi-step workflows, output parsers for structured extraction, CIDR network discovery, and SSH tunneling.

## Why Herd

Tools like `pssh`, `pdsh`, and `ansible` can run commands across hosts, but none of them group identical output or show diffs between hosts. Herd treats identical output as the norm and surfaces outliers, so you can instantly see which hosts match and which differ -- then drill into the outliers without leaving the terminal.

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
# One-shot command
herd exec "cat /etc/os-release | grep PRETTY" pi-garage pi-livingroom pi-workshop

# Interactive REPL with persistent connections
herd pi-garage pi-livingroom pi-workshop --insecure
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

### Exec

Run a single command and exit. Pipe-friendly.

```
herd exec [command] [hosts...] [flags]
```

#### Exec Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--group` | `-g` | Use a host group from config |
| `--concurrency` | | Max parallel connections (default 20) |
| `--timeout` | | Per-host timeout, e.g. `30s`, `1m` (default 30s) |
| `--json` | | Output results as JSON |
| `--errors-only` | | Only show failed hosts |
| `--insecure` | | Skip host key verification |
| `--sudo` | | Run commands with sudo |
| `--ask-become-pass` | | Prompt for sudo password |
| `--parse` | | Parse output with a named parser (built-in: `disk`, `free`, `uptime`) |

#### Exec Examples

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

# Run a privileged command with sudo
herd exec "systemctl restart nginx" -g web --sudo --ask-become-pass

# Preview what would run without connecting
herd exec "uname -r" -g pis --dry-run

# Parse disk usage into a table
herd exec "df -h" -g pis --parse disk

# Parse memory usage into a table
herd exec "free -h" -g pis --parse free

# Parse uptime into a table
herd exec "uptime" -g pis --parse uptime
```

### Interactive REPL

Start a persistent session with SSH connections kept open across commands. Run a command, see grouped results, then use selectors to drill into subsets.

```bash
# With a host group
herd -g pis --insecure

# With hosts on the command line
herd pi-garage pi-livingroom pi-workshop --insecure

# Start with sudo enabled
herd -g pis --sudo --ask-become-pass
```

#### REPL Session Example

```
herd [pis: 4 hosts]> uptime
 3 hosts identical:
   pi-garage, pi-livingroom, pi-workshop
   12:34:56 up 14 days, 3:22, 0 users, load average: 0.02, 0.05, 0.01

 1 host differs:
   pi-backyard
   12:34:56 up 3 days, 1:15, 0 users, load average: 0.45, 0.38, 0.22

4 succeeded

herd [pis: 4 hosts]> @differs df -h /
 1 host identical:
   pi-backyard
   /dev/sda1  28G  26G  1.2G  96%  /

1 succeeded

herd [pis: 4 hosts]> :sudo
BECOME password:
sudo mode enabled

herd [pis: 4 hosts]> @pi-backyard apt autoremove -y
 1 host identical:
   pi-backyard
   [output...]

1 succeeded

herd [pis: 4 hosts]> :history
 1    uptime                                     (4 hosts, 3 ok, 1 differs)
 2    @differs df -h /                           (1 host, 1 ok)
 3    @pi-backyard apt autoremove -y             (1 host, 1 ok)

herd [pis: 4 hosts]> :quit
```

#### Selectors

Prefix a command with a selector to target a subset of hosts based on the previous command's results:

| Selector | Description |
|----------|-------------|
| `@all` | All hosts in the current group (default when no selector) |
| `@ok` | Hosts that succeeded and matched the majority output |
| `@differs` | Hosts whose output differed from the majority |
| `@failed` | Hosts with non-zero exit codes or connection errors |
| `@timeout` | Hosts that timed out |
| `@hostname` | Exact hostname match |
| `@glob-*` | Glob pattern match (e.g. `@pi-*`, `@web-0[12]`) |

Selectors can be combined with commas: `@differs,@failed`

#### REPL Commands

| Command | Description |
|---------|-------------|
| `:quit` / `:q` | Exit the REPL |
| `:history` / `:h` | Show command history with result summaries |
| `:hosts` | List all hosts with connection status |
| `:group <name>` | Switch to a different host group |
| `:timeout <duration>` | Change the per-host timeout |
| `:diff` | Show full diff of last command's divergent output |
| `:last` | Re-display the last command's results |
| `:export <file>` | Export last results to a JSON file |
| `:sudo` | Toggle sudo mode on/off (prompts for password when enabling) |
| `:recipe [name]` | Run a recipe, or list available recipes if no name given |
| `:parse <name>` | Re-parse last command output with a named parser |

### Push & Pull (SFTP File Transfer)

Transfer files to or from multiple hosts in parallel over SFTP.

```bash
# Push a local file to all hosts in a group
herd push ./config.yaml:/etc/app/config.yaml -g webservers

# Pull a remote file from all hosts (saved to ./results/<hostname>/)
herd pull /var/log/syslog -g webservers

# Pull to a custom directory
herd pull /etc/nginx/nginx.conf -g web --dest ./configs
```

Output includes per-host byte count, partial SHA-256 checksum, and transfer time:

```
  web-01  4096 bytes  a1b2c3d4e5f6  12ms
  web-02  4096 bytes  a1b2c3d4e5f6  15ms
  web-03  4096 bytes  a1b2c3d4e5f6  11ms

3 succeeded, 0 failed
```

### Ping

Check TCP reachability of hosts without performing an SSH handshake. Fast connectivity check.

```bash
herd ping -g pis
herd ping web-01 web-02 web-03 --timeout 10s
```

```
  pi-garage                      reachable (12ms)
  pi-livingroom                  reachable (8ms)
  pi-workshop                    unreachable (connection refused)

2/3 hosts reachable
```

### Dashboard

Launch a full-screen TUI for interactive fleet monitoring. Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea), the dashboard shows a host table, command input, and a tabbed output pane in a single terminal.

```bash
herd dashboard -g pis --insecure
herd dashboard -g web --sudo --ask-become-pass --health-interval 30s
```

| Flag | Description |
|------|-------------|
| `--health-interval` | Interval between health checks (default `10s`) |

The output pane uses tabs to switch between the grouped diff view and individual host output. After running a command, a **Diff** tab shows the grouped/diff summary and one tab per host shows that host's raw output.

#### Dashboard Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `Tab` | Cycle focus: command input → host table → output pane |
| `Enter` | Execute command (input) / jump to host tab (host table) |
| `Esc` | Close overlay / return to diff tab |
| `[` / `]` | Previous / next output tab |
| `1`–`9` | Jump to output tab by number |
| `j` / `k` | Navigate host table up/down |
| `f` | Toggle host filter bar |
| `d` | Show diff for selected divergent host |
| `?` | Toggle help overlay |
| `q` / `Ctrl+C` | Quit |

### Recipes

Recipes are named multi-step command sequences defined in your config file. Each step runs sequentially, and selectors like `@ok`, `@differs`, and `@failed` in later steps reference the previous step's results.

```bash
# List available recipes
herd recipe --list

# Run a recipe
herd recipe deploy -g web

# Run with sudo
herd recipe restart-stack -g pis --sudo --ask-become-pass
```

#### Recipe Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--list` | | List available recipes |
| `--group` | `-g` | Use a host group from config |
| `--concurrency` | | Max parallel connections (default 20) |
| `--timeout` | | Per-host timeout |
| `--insecure` | | Skip host key verification |
| `--sudo` | | Run commands with sudo |
| `--ask-become-pass` | | Prompt for sudo password |

#### Recipe Example

With this config:

```yaml
recipes:
  deploy:
    description: "Deploy and verify"
    steps:
      - "git -C /opt/app pull"
      - "systemctl restart app"
      - "@failed systemctl status app"
```

Running `herd recipe deploy -g web` will:
1. Pull latest code on all hosts
2. Restart the app service on all hosts
3. Show service status only for hosts where the restart failed

#### Recipe Output

```
=== Step 1/3: git -C /opt/app pull ===
 3 hosts identical:
   web-01, web-02, web-03
   Already up to date.

=== Step 2/3: systemctl restart app ===
 2 hosts identical:
   web-01, web-02
   [ok]

 1 host differs:
   web-03
   Job for app.service failed.

=== Step 3/3: systemctl status app ===
    Selector: @failed → 1 host
 1 host identical:
   web-03
   ● app.service - App
   ...
```

### Output Parsers

Parse command output into structured tables. Herd includes built-in parsers for common commands and supports custom parsers in the config file.

```bash
# Parse disk usage into a table
herd exec "df -h" -g pis --parse disk

# Parse memory info
herd exec "free -h" -g pis --parse free

# Parse uptime and load averages
herd exec "uptime" -g pis --parse uptime
```

#### Parser Output

```
HOST            FILESYSTEM  SIZE  USED  AVAIL  USE_PCT  MOUNT
--------------  ----------  ----  ----  -----  -------  -----
pi-garage       /dev/sda1   50G   20G   28G    42%      /
pi-livingroom   /dev/sda1   50G   18G   30G    38%      /
pi-workshop     /dev/sda1   50G   45G   3.2G   93%      /
```

In the REPL, use `:parse` to re-parse the last command's output:

```
herd [pis: 4 hosts]> uptime
...
herd [pis: 4 hosts]> :parse uptime
HOST           UPTIME            USERS  LOAD1  LOAD5  LOAD15
-------------  ----------------  -----  -----  -----  ------
pi-garage      14 days,  3:22    2      0.02   0.05   0.01
pi-livingroom  14 days,  3:20    1      0.01   0.03   0.00
pi-workshop    3 days,  1:15     1      0.45   0.38   0.22
```

#### Built-in Parsers

| Name | Command | Fields |
|------|---------|--------|
| `disk` | `df -h` | filesystem, size, used, avail, use_pct, mount |
| `free` | `free -h` | total, used, free, available |
| `uptime` | `uptime` | uptime, users, load1, load5, load15 |

Custom parsers can be defined in the config file (see [Configuration](#configuration)).

### Discover

Scan a network range for SSH hosts. Useful for initial setup or finding new hosts on a network.

```bash
# Scan a /24 subnet for SSH hosts
herd discover --cidr 192.168.1.0/24

# Scan a custom port with higher concurrency
herd discover --cidr 10.0.0.0/16 --port 2222 --concurrency 100 --timeout 5s

# Save discovered hosts to a config group
herd discover --cidr 192.168.1.0/24 --save lab
```

#### Discover Flags

| Flag | Description |
|------|-------------|
| `--cidr` | CIDR range to scan (required) |
| `--port` | TCP port to probe (default 22) |
| `--timeout` | Per-host connection timeout (default 2s) |
| `--concurrency` | Max concurrent connections (default 50) |
| `--save` | Save discovered hosts to a named group in config |

#### Discover Output

```
Scanning 192.168.1.0/24 port 22...
  192.168.1.10:22
  192.168.1.15:22
  192.168.1.20:22

3 hosts found
```

When `--save` is used, discovered hosts are added to (or replace) the named group in your config file.

### Tunnel

Create local SSH tunnels (port forwarding) to multiple hosts simultaneously. Each host gets an incrementing local port.

```bash
# Forward local port 8080 to port 80 on each host
herd tunnel -L 8080:localhost:80 -g web

# Forward to a database port
herd tunnel -L 3306:localhost:3306 -g db
```

#### Tunnel Flags

| Flag | Short | Description |
|------|-------|-------------|
| `-L` | | Forward spec: `localPort:remoteHost:remotePort` (required) |
| `--group` | `-g` | Use a host group from config |
| `--concurrency` | | Max concurrent connections (default 20) |
| `--timeout` | | Per-host timeout |
| `--insecure` | | Skip host key verification |

#### Tunnel Output

With 3 hosts in the `web` group:

```
Opening tunnels...
  127.0.0.1:8080 → web-01 → localhost:80
  127.0.0.1:8081 → web-02 → localhost:80
  127.0.0.1:8082 → web-03 → localhost:80

3 tunnels active. Press Ctrl-C to close.
```

Each host gets its own local port, incrementing from the base port in the `-L` spec.

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

### Utility Commands

| Command | Description |
|---------|-------------|
| `herd list` | List all configured host groups and their members |
| `herd config` | Show the resolved configuration as YAML |
| `herd discover --cidr <range>` | Scan a network for SSH hosts |
| `herd version` | Print version, commit, and build date |
| `herd completion [bash\|zsh\|fish\|powershell]` | Generate shell completion scripts |

### Global Flags

| Flag | Description |
|------|-------------|
| `--dry-run` | Show resolved hosts and what would be executed, without connecting |

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

recipes:
  deploy:
    description: "Deploy and verify"
    steps:
      - "git -C /opt/app pull"
      - "systemctl restart app"
      - "@failed systemctl status app"
  health-check:
    description: "Quick health check"
    steps:
      - "systemctl is-active nginx"
      - "curl -s localhost:8080/health"

parsers:
  nginx-conns:
    description: "Parse nginx active connections"
    extract:
      - field: active
        pattern: 'Active connections:\s+(\d+)'
      - field: requests
        pattern: '\s+(\d+)\s+\d+\s+\d+\s*$'
```

Groups support per-group `user` and `timeout` overrides. Recipe names and parser names must match `[a-zA-Z0-9_-]+`.

### SSH Config

Herd reads `~/.ssh/config` and resolves `Host`, `User`, `Port`, `IdentityFile`, and `ProxyJump` for each host. Hosts not defined in the herd config will still work if they are in your SSH config.

### Authentication

Herd tries authentication methods in this order:

1. SSH agent (via `SSH_AUTH_SOCK`)
2. Key files (from `~/.ssh/config` IdentityFile or default locations)
3. Password prompt (interactive terminal only)

The password is prompted once and cached for the session.

### Shell Completions

```bash
# Bash
source <(herd completion bash)

# Zsh
herd completion zsh > "${fpath[1]}/_herd"

# Fish
herd completion fish | source
```

Group names are completed dynamically for the `-g` flag.

## Exit Codes

- `0` if all hosts succeed
- `1` if any host fails or returns a non-zero exit code

## Architecture

```
internal/
  config/       Config file parsing, host group resolution, SSH config merging
  ssh/          SSH client, connection pool, auth chain, sudo, ProxyJump support
  executor/     Parallel command execution with bounded concurrency
  grouper/      Output hashing, grouping by identical output, unified diffing
  selector/     @-selector parsing and resolution against last results
  transfer/     SFTP push/pull with parallel transfers and checksum verification
  recipe/       Multi-step recipe runner with selector propagation
  parser/       Output field extraction with regex/column rules and table formatting
  discover/     CIDR network scanning for SSH host discovery
  tunnel/       SSH port forwarding (local tunnels) with multi-host support
  ui/
    exec/       Terminal output formatting (grouped, JSON, errors-only)
    repl/       Interactive REPL with persistent connections and history
    dashboard/  Full-screen TUI dashboard (Bubble Tea)
```

## License

MIT
