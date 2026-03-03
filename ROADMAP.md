# Herd Roadmap

### What's next

The following phases take herd from "powerful multi-host SSH tool" to "indispensable daily driver for fleet operations." Each phase builds on the previous, and the dependency chain is designed so every phase ships standalone value.

```
Phase 5: Tags ─────► Phase 6: Watch + History ─────► Phase 7: Tail + Notifications
                              │                                │
                              ▼                                ▼
                     Phase 8: Snapshots + Diffing     Phase 9: v1.0 Hardening
```

---

## Phase 5: Host Tags and Dynamic Inventory (v0.5.0)

**Goal:** Cross-cutting host selection that goes beyond flat groups.

**Why:** As fleets grow, hosts belong to multiple categories (OS version, role, location, environment). Tags allow querying across all groups by attribute — the difference between "small team tool" and "production fleet tool."

### 5.1 Host tags in config

- [ ] Extend host definition to support tags in config YAML
- [ ] Tags as flat string list per host: `tags: [debian12, arm64, indoor, prod]`
- [ ] Shorthand syntax alongside existing bare string format
- [ ] Backward-compatible: bare hostname strings still work (no tags = empty list)

```yaml
groups:
  pis:
    hosts:
      - host: pi-garage
        tags: [debian12, arm64, indoor]
      - host: pi-workshop
        tags: [debian11, arm64, outdoor]
      - pi-backyard  # bare string still works, no tags
  web:
    hosts:
      - host: web-01
        tags: [nginx, prod, us-east]
      - host: web-02
        tags: [nginx, prod, us-west]
      - host: web-03
        tags: [nginx, staging]
```

### 5.2 Tag-based CLI selection

- [ ] `--tag` / `-t` flag on exec, recipe, push, pull, ping, dashboard, tunnel
- [ ] Tag selects across ALL groups (flattened, deduplicated)
- [ ] Multiple tags with comma = AND logic: `--tag debian12,arm64`
- [ ] Negation with `!`: `--tag '!staging'` excludes hosts with that tag
- [ ] `--tag` can be combined with `--group` (intersection)

```bash
herd exec "uname -r" --tag debian12
herd exec "apt upgrade -y" --tag prod,debian12
herd exec "df -h" --tag '!staging'
herd exec "uptime" -g pis --tag arm64     # intersection: pis group AND arm64 tag
```

### 5.3 Tag selectors in REPL

- [ ] New selector: `@tag:tagname` to target hosts with a specific tag
- [ ] Combinable with existing selectors: `@differs,@tag:prod`
- [ ] `:tags` REPL command to list all known tags and host counts
- [ ] Tag completion in REPL tab-complete

```
herd [all: 8 hosts]> @tag:prod uptime
 4 hosts identical:
   web-01, web-02, pi-garage, pi-workshop
   ...

herd [all: 8 hosts]> @differs,@tag:nginx systemctl status nginx
```

### 5.4 Auto-tagging from discover

- [ ] `herd discover --cidr 192.168.1.0/24 --save lab --tag discovered,lan`
- [ ] Discovered hosts get user-supplied tags applied automatically

### 5.5 Tag listing and management

- [ ] `herd list --tags` shows all tags with host counts
- [ ] `herd list --tag debian12` shows only hosts matching tag
- [ ] Dashboard host table shows tags column (toggleable with `t` key)

**Milestone:** `herd exec "uname -r" --tag prod,debian12` selects hosts across all groups by tag.

**Estimated scope:** ~800 LOC across config, selector, and command packages.

---

## Phase 6: Watch Mode and Temporal Awareness (v0.6.0)

**Goal:** Detect changes across hosts over time, not just across hosts at a point in time.

**Why:** The #1 question sysadmins ask is "what changed?" Herd already answers "what differs between hosts" — this phase answers "what differs from last time." No competing tool does this.

### 6.1 Persistent history with SQLite

- [ ] New `internal/history/` package with SQLite storage (modernc.org/sqlite — pure Go, no CGO)
- [ ] Schema:
  - `runs` table: id, command, group_name, tags, started_at, finished_at, host_count, success_count, fail_count
  - `results` table: run_id, host, stdout, stderr, exit_code, duration_ms, output_hash
- [ ] Auto-record every exec/recipe/watch run
- [ ] Configurable retention: `defaults.history_retention: 30d` (default 30 days)
- [ ] Database location: `~/.local/share/herd/history.db`
- [ ] Automatic migration on schema changes

### 6.2 History commands

- [ ] `herd history` — list recent runs with summary (command, host count, success/fail, timestamp)
- [ ] `herd history show <id>` — replay a past run's grouped output
- [ ] `herd history diff <id1> <id2>` — unified diff between two runs of the same command
- [ ] `:history search <pattern>` in REPL — search past commands across sessions
- [ ] `herd history export <id> --json` — export a run's full results

```bash
$ herd history
 ID   COMMAND                        GROUP  HOSTS  OK  FAIL  TIME
 142  systemctl is-active nginx      web    3      3   0     2026-02-28 09:15
 141  df -h / | tail -1              pis    4      4   0     2026-02-28 09:10
 140  uname -r                       pis    4      3   1     2026-02-27 14:30
 ...

$ herd history diff 140 142
```

### 6.3 Watch mode

- [ ] `herd watch <command> [hosts...] [flags]` — run a command at regular intervals
- [ ] `--interval` flag (default 30s, minimum 5s)
- [ ] Each iteration runs through the standard executor + grouper pipeline
- [ ] Display: clear screen, show grouped output with timestamp header
- [ ] Change detection: compare current iteration's output hashes to previous iteration
- [ ] Highlight hosts that changed since last iteration (new status category: `@changed`)
- [ ] Every iteration recorded to history DB

```bash
# Watch disk usage every 60 seconds
herd watch "df -h / | tail -1" -g pis --interval 60s

# Watch a service across the web tier
herd watch "systemctl is-active nginx" -g web --interval 10s

# Watch with tag-based selection
herd watch "free -h | grep Mem" --tag prod --interval 30s
```

### 6.4 Watch output modes

- [ ] Default: full grouped output each iteration, `[changed]` marker on hosts that differ from previous
- [ ] `--changes-only`: only print output when a host's output changes from the previous iteration
- [ ] `--json`: stream JSON objects per iteration for piping to jq/scripts
- [ ] `--count <n>`: run N iterations then exit (default: infinite until Ctrl-C)

### 6.5 Temporal diff (--diff-last)

- [ ] `--diff-last` flag on `herd exec`: compare current results against the most recent matching run in history
- [ ] Matching = same command string + same group/tag selection
- [ ] Shows per-host diff of what changed since last time
- [ ] Answers "what packages changed?" / "what config drifted?"

```bash
$ herd exec "rpm -qa | sort" -g web --diff-last

 3 hosts unchanged since last run (2026-02-27 14:30)

 1 host changed:
   web-03
   --- 2026-02-27 14:30
   +++ 2026-02-28 09:15
   +nginx-1.26.0
   -nginx-1.25.3

3 succeeded, 1 changed
```

### 6.6 Watch mode in dashboard

- [ ] Dashboard command bar accepts `watch <command>` prefix
- [ ] Watch iterations update host table status and output pane in real-time
- [ ] Changed hosts highlighted in host table (yellow/amber indicator)
- [ ] Stop watch with Esc or by entering a new command

**Milestone:** `herd watch "systemctl is-active nginx" -g web --interval 10s` shows live fleet status with change detection.

**Estimated scope:** ~1500 LOC. SQLite history is the largest piece; watch mode reuses executor + grouper.

---

## Phase 7: Log Tailing and Notifications (v0.7.0)

**Goal:** Real-time log streaming and alerting for production use.

**Why:** Log tailing across hosts is one of the most common fleet operations. Nerdlog proved the demand, but it's a separate tool. Integrating tailing into herd — with the same SSH pool, same tag/group selection, same dashboard — creates a unified workflow. Notifications close the loop: watch for problems, get alerted automatically.

### 7.1 Multi-host log tailing

- [ ] `herd tail <remote-path> [hosts...] [flags]` — stream remote log files via SSH
- [ ] Uses `tail -f` over persistent SSH connection (streamed output, not buffered)
- [ ] Output format: `[hostname] log line` with per-host color coding
- [ ] Chronological merge of lines across hosts (best-effort, based on receive time)
- [ ] `--lines <n>` flag for initial context (default 10)
- [ ] Graceful Ctrl-C to stop all streams

```bash
# Tail syslog across web tier
herd tail /var/log/syslog -g web

# Tail with filtering
herd tail /var/log/nginx/error.log -g web --grep "502|503"

# Tail journalctl output
herd tail --journalctl "nginx.service" -g web --since "5m"

# Tail multiple log files
herd tail /var/log/auth.log /var/log/syslog -g pis
```

### 7.2 Tail filtering and search

- [ ] `--grep <pattern>` — filter lines matching regex (applied remotely via `grep -E` for bandwidth efficiency)
- [ ] `--exclude <pattern>` — exclude lines matching pattern (remote `grep -v`)
- [ ] `--since <duration>` — only show lines from the last N minutes/hours (journalctl mode)
- [ ] `--journalctl <unit>` — use `journalctl -f -u <unit>` instead of `tail -f`
- [ ] `--no-hostname` — omit hostname prefix (useful when tailing a single host)

### 7.3 Tail in dashboard

- [ ] Dashboard supports `tail <path>` in command bar
- [ ] Streaming output rendered in output pane with per-host tabs
- [ ] Diff tab shows line rate per host (which hosts are noisiest)
- [ ] Filter bar applies to streaming output in real-time
- [ ] `Esc` or new command stops the tail

### 7.4 Notifications and webhooks

- [ ] Config-driven notification system in `~/.config/herd/config.yaml`
- [ ] New `internal/notify/` package with pluggable notification backends
- [ ] Trigger events: `failure` (non-zero exit), `change` (output differs from previous run), `timeout`
- [ ] Notification targets:
  - **webhook**: HTTP POST to any URL with configurable headers and JSON body template
  - **slack**: Slack incoming webhook with formatted message
  - **script**: Execute a local command with environment variables for context

```yaml
notifications:
  - name: slack-alerts
    type: slack
    webhook: "https://hooks.slack.com/services/T.../B.../xxx"
    on: [failure, change]

  - name: pagerduty
    type: webhook
    url: "https://events.pagerduty.com/v2/enqueue"
    method: POST
    headers:
      Content-Type: application/json
    body: |
      {
        "routing_key": "YOUR_KEY",
        "event_action": "trigger",
        "payload": {
          "summary": "{{.Summary}}",
          "source": "herd",
          "severity": "error",
          "custom_details": {
            "command": "{{.Command}}",
            "failed_hosts": "{{.FailedHosts}}"
          }
        }
      }
    on: [failure]

  - name: local-script
    type: script
    command: "/usr/local/bin/alert.sh"
    on: [failure, timeout]
```

### 7.5 Watch + notifications integration

- [ ] `herd watch` triggers notifications when configured events occur
- [ ] `--notify` flag to enable notifications for one-off exec commands
- [ ] Rate limiting: configurable cooldown per notification target (default 5m) to avoid alert storms
- [ ] `--notify-on-resolve`: send follow-up notification when a previously failed host recovers

**Milestone:** `herd tail /var/log/syslog -g web --grep "error"` streams logs; `herd watch` triggers Slack alerts on failures.

**Estimated scope:** ~1200 LOC. Tail reuses SSH pool; notify is a new package with 3 backends.

---

## Phase 8: Fleet Snapshots and Remote File Diffing (v0.8.0)

**Goal:** Capture and compare fleet state over time; detect config drift.

**Why:** Snapshots answer "what does my fleet look like right now?" and remote file diffing answers "are all my configs the same?" — two questions that currently require ad-hoc scripting. This phase turns herd into a lightweight fleet auditing tool.

### 8.1 Fleet snapshots

- [ ] `herd snapshot -g <group>` — capture OS, packages, services, disk, memory across hosts
- [ ] Runs a built-in recipe of system inspection commands (uname, df, free, systemctl list-units, etc.)
- [ ] Stores snapshot in history DB with structured metadata per host
- [ ] `herd snapshot list` — list past snapshots with group, host count, timestamp
- [ ] `herd snapshot show <id>` — display a snapshot's data in table format
- [ ] `herd snapshot diff <id1> <id2>` — diff two snapshots, highlight what changed per host
- [ ] `herd snapshot export <id> --json` — export snapshot to JSON file

```bash
$ herd snapshot -g pis
Capturing snapshot of 4 hosts...
  OS info .............. done
  Disk usage ........... done
  Memory ............... done
  Services ............. done

Snapshot #23 saved (4 hosts, 2026-02-28 10:00)

$ herd snapshot diff 20 23
 3 hosts unchanged

 1 host changed (pi-workshop):
   disk_used: 42% → 93%
   packages: +2 (nginx-1.26, curl-8.5)
```

### 8.2 Remote file diffing

- [ ] `herd diff <remote-path> -g <group>` — pull a file from all hosts into memory and show diffs
- [ ] Groups hosts with identical file content (same SHA-256 pattern as command output)
- [ ] Shows unified diff between the majority file and outliers
- [ ] Useful for config drift detection

```bash
$ herd diff /etc/ssh/sshd_config -g pis

 3 hosts identical:
   pi-garage, pi-livingroom, pi-workshop
   [first 5 lines...]

 1 host differs:
   pi-backyard
   --- norm (3 hosts)
   +++ pi-backyard
   -PermitRootLogin no
   +PermitRootLogin yes

4 files compared, 1 differs
```

- [ ] `--save <dir>` flag to also save all files locally (like `herd pull` but with diffing)
- [ ] Works in REPL: `:diff /etc/nginx/nginx.conf` as a REPL command
- [ ] Dashboard: `diff <path>` in command bar shows grouped file diff in output pane

### 8.3 Recipe variables and templating

- [ ] Recipes accept `params` with default values
- [ ] Template syntax: `{{.param_name}}` in step commands
- [ ] Pass params via CLI: `herd recipe deploy -g web --set branch=main --set service=nginx`
- [ ] Built-in variables: `{{.Host}}`, `{{.Group}}`, `{{.Timestamp}}`
- [ ] Validation: error if required param not provided and no default

```yaml
recipes:
  deploy:
    description: "Deploy a branch and restart a service"
    params:
      branch:
        default: main
        description: "Git branch to deploy"
      service:
        required: true
        description: "Systemd service name"
    steps:
      - "git -C /opt/{{.service}} checkout {{.branch}} && git pull"
      - "systemctl restart {{.service}}"
      - "@failed systemctl status {{.service}}"
```

```bash
herd recipe deploy -g web --set service=myapp --set branch=release/2.0
```

**Milestone:** `herd diff /etc/nginx/nginx.conf -g web` detects config drift; `herd snapshot` captures fleet state.

**Estimated scope:** ~1000 LOC. Snapshot is a specialized recipe + history integration; file diff reuses grouper.

---

## Phase 9: Production Hardening (v1.0.0)

**Goal:** Stability, performance, packaging, and documentation for a v1.0 release.

### 9.1 Performance and reliability

- [ ] ControlMaster-style SSH multiplexing for repeated exec calls
- [ ] Connection health watchdog: proactive stale connection detection and reconnection
- [ ] Benchmark suite: measure execution latency for 10, 50, 100, 500 host scenarios
- [ ] Memory profiling for large fleets (ensure output buffering scales)
- [ ] History DB vacuum and WAL mode for concurrent read/write safety

### 9.2 Release engineering

- [ ] goreleaser config for multi-platform builds (darwin/amd64, darwin/arm64, linux/amd64, linux/arm64)
- [ ] Homebrew tap formula
- [ ] AUR package
- [ ] Nix flake
- [ ] Installation script (curl | sh)
- [ ] GitHub releases with changelogs
- [ ] VHS-recorded terminal GIFs for README (exec, REPL, dashboard, watch, tail)

### 9.3 Documentation

- [ ] Man page generation via cobra
- [ ] `herd help <topic>` for detailed per-feature docs (topics: selectors, recipes, parsers, tags, watch, tail, notifications)
- [ ] Examples directory with common workflows
- [ ] Config reference with all options documented
- [ ] Migration guide from pssh/pdsh/ansible ad-hoc

### 9.4 Config management

- [ ] `herd config export` / `herd config import` for sharing configurations
- [ ] Git-friendly YAML format (sorted keys, stable output)
- [ ] Optional: config directory mode (`~/.config/herd/groups/*.yaml`) for modular group definitions
- [ ] Config validation command: `herd config validate`

**Milestone:** v1.0.0 release — stable, documented, packaged for all major platforms.

---

## Non-Goals (Scope Guard)

These are explicitly out of scope for herd. If a feature creeps toward any of these, reconsider:

- Ansible-style declarative playbooks or idempotent state management
- Configuration management (Chef, Puppet, Salt territory)
- Infrastructure-as-code (Terraform, Pulumi territory)
- Agent installation on target hosts (herd is agentless, SSH-only)
- Web UI or REST API (herd is a terminal tool)
- Cloud provider integrations (AWS, GCP, Azure API calls)
- Container orchestration (Kubernetes, Docker Swarm territory)
- User/team management or RBAC (single-user CLI tool)

Herd runs commands, shows results, detects drift, and alerts on changes. That's it.

---

## Summary: Build Order and Dependencies

| Phase | Version | Feature | Depends On | Key New Packages |
|-------|---------|---------|------------|------------------|
| **5** | v0.5.0 | Host Tags + Dynamic Inventory | — | config (extend), selector (extend) |
| **6** | v0.6.0 | Watch Mode + Persistent History | Phase 5 (tags in history) | `internal/history/` |
| **7** | v0.7.0 | Log Tailing + Notifications | Phase 6 (history for notify state) | `internal/notify/` |
| **8** | v0.8.0 | Fleet Snapshots + File Diffing | Phase 6 (history storage) | snapshot (new recipe), diff (new cmd) |
| **9** | v1.0.0 | Production Hardening | All above | benchmark, docs, packaging |

**Recommended first pick: Phase 5 (Tags)** — low effort, high leverage, makes every subsequent phase more powerful.
