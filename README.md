# pxve

A command-line tool for managing Proxmox VE infrastructure — virtual machines,
containers, nodes, users, and access control.

## Features

- **VMs & containers** — list, start, stop, reboot, shutdown, clone, delete, snapshots
- **Nodes & cluster** — status, resources, running tasks
- **Users & tokens** — create, delete, password, API token management
- **ACLs** — grant and revoke roles on VMs, containers, or arbitrary paths
- **Multi-instance** — manage multiple Proxmox servers with named profiles
- **Output formats** — human-readable tables or `--output json`
- **Interactive TUI** — full-screen terminal UI for browsing and managing instances, VMs, containers, users, and snapshots

## Quick Start

```sh
# Add a Proxmox instance (verifies connectivity on add)
pxve instance add home-lab \
  --url https://192.168.1.10:8006 \
  --token-id root@pam!cli \
  --token-secret <secret>

# Set it as the default
pxve instance use home-lab

# List VMs
pxve vm list

# Start a VM
pxve vm start 101

# Take a snapshot
pxve vm snapshot create 101 before-upgrade

# Create a user and grant VM access
pxve user create alice@pve --password secret
pxve user grant alice@pve --vmid 101 --role PVEVMUser
```

## Interactive TUI

Launch the interactive terminal UI with:

```sh
pxve --tui
```

The TUI provides a keyboard-driven interface for managing your Proxmox
infrastructure without memorizing CLI subcommands. It reads instance
profiles from `~/.pxve.yaml` and lets you:

- **Select an instance** — pick from configured instances, add or remove instances inline
- **Browse VMs & containers** — sortable table with status, CPU, memory, and disk usage
- **Manage snapshots** — create, delete, and rollback snapshots from the detail view
- **Power actions** — start, stop, shutdown, and reboot directly from the detail view
- **Manage users** — list, create, and delete Proxmox users
- **Manage tokens & ACLs** — create/delete API tokens, grant/revoke ACL roles per user

Navigation: **Enter** to select, **Esc** to go back, **Tab** to switch between
VMs and Users views, **Q** or **Ctrl+C** to quit.

## Instance Management

Instances are stored in `~/.pxve.yaml`. Authentication supports both API
tokens (recommended) and username/password.

```sh
pxve instance add <name> --url <url> --token-id '<id>' --token-secret <secret>
pxve instance add <name> --url <url> --username root@pam --password <pass>
pxve instance list
pxve instance use <name>
pxve instance show [name]
pxve instance remove <name>
```

TLS certificate verification is **skipped by default** (most Proxmox nodes use
self-signed certs). Add `--secure` to enforce certificate verification for instances
with a valid certificate chain.

For one-off commands without saving an instance:

```sh
pxve vm list --url https://host:8006 --token-id root@pam!cli --token-secret <secret>
```

## Building

Requires Go 1.21+.

```sh
make build          # builds both targets into dist/
make macos-arm      # dist/pxve-macos-arm64
make linux-amd64    # dist/pxve-linux-amd64
make clean          # removes dist/
```

Binaries are fully static (`CGO_ENABLED=0`) with no runtime dependencies.

## Command Reference

### VMs and Containers

`vm` and `ct` (alias: `container`) support the same set of subcommands:

```
pxve vm | ct  list                          [--node <node>]
pxve vm | ct  start    <id>                 [--node <node>]
pxve vm | ct  stop     <id>                 [--node <node>]
pxve vm | ct  shutdown <id>                 [--node <node>]
pxve vm | ct  reboot   <id>                 [--node <node>]
pxve vm | ct  info     <id>                 [--node <node>]
pxve vm | ct  clone    <id> <name>          [--node <node>] [--newid <id>]
pxve vm | ct  delete   <id>                 [--node <node>]
pxve vm | ct  snapshot list     <id>        [--node <node>]
pxve vm | ct  snapshot create   <id> <name> [--node <node>]
pxve vm | ct  snapshot rollback <id> <name> [--node <node>]
pxve vm | ct  snapshot delete   <id> <name> [--node <node>]
```

> **Note:** `vm shutdown` sends an ACPI signal (guest-initiated). `ct shutdown`
> sends an orderly shutdown request to the container runtime. Both are graceful;
> `stop` is always forceful.

### Nodes & Cluster

```
pxve node list
pxve node info <node>

pxve cluster status
pxve cluster resources
pxve cluster tasks
```

### Users & Access

```
pxve user list
pxve user create <userid> [--password <pw>] [--email <e>] [--firstname <f>] [--lastname <l>]
pxve user delete <userid>
pxve user password <userid> --password <new>
pxve user grant  <userid> --vmid <id>[,<id>] --role <role>
pxve user grant  <userid> --path /storage/local --role PVEDatastoreUser
pxve user revoke <userid> --vmid <id> --role <role>
pxve user token list   <userid>
pxve user token create <userid> <tokenid>
pxve user token delete <userid> <tokenid>

pxve acl list [--user <userid>]
pxve role list
```

### Global Flags

Available on every command:

```
-i, --instance <name>    use a named instance from config
    --output json         output as JSON instead of table
    --secure              enforce TLS certificate verification (default: skip)
```

### Root Flags

```
    --tui                 launch interactive terminal UI
```
