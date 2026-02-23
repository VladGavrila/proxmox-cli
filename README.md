# pxve

A command-line tool for managing Proxmox VE infrastructure — virtual machines,
containers, nodes, users, and access control.

## Features

- **VMs & containers** — list, start, stop, reboot, shutdown, clone, delete, snapshots, convert to template, disk resize, disk move, tag management
- **Guest agent** — execute commands, query OS info and network interfaces, set passwords inside running VMs via QEMU guest agent
- **Backups** — list, create (vzdump), delete, restore, inspect embedded config, storage discovery
- **Nodes & cluster** — status, resources, running tasks
- **Users & tokens** — create, delete, password, API token management
- **ACLs** — grant and revoke roles on VMs, containers, or arbitrary paths
- **Multi-instance** — manage multiple Proxmox servers with named profiles
- **Instance discovery** — scan any subnet for Proxmox instances on port 8006
- **Output formats** — human-readable tables or `--output json`
- **Interactive TUI** — full-screen terminal UI for browsing and managing instances, VMs, containers, backups, users, and snapshots

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

# Create a backup
pxve backup create 101 --storage local

# List backups
pxve backup list --vmid 101

# Restore a backup to a new VM
pxve backup restore local:backup/vzdump-qemu-101-2025_01_01-00_00_00.vma.zst \
  --node pve --vmid 200 --name restored-vm --storage local-lvm

# Query guest OS info via agent
pxve vm agent osinfo 101

# Run a command inside the VM
pxve vm agent exec 101 -- uname -a

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

- **Select an instance** — pick from configured instances, add, remove, or discover instances inline
- **Browse VMs & containers** — sortable table with status, CPU, memory, and disk usage; detail view shows primary disk storage in the stats line
- **Power actions** — start, stop, shutdown, reboot, clone, delete, convert to template, resize disks, move disks between storages, and manage tags directly from the list or detail view
- **Guest agent info** — for QEMU VMs with `qemu-guest-agent` running, the detail view shows the guest OS name and primary IP address
- **Manage snapshots** — create, delete, and rollback snapshots from the detail view
- **Manage backups** — create, delete, and restore backups with storage selection and VMID/name prompts
- **Browse all backups** — cluster-wide backup view across all nodes and storages with delete and restore
- **Manage users** — list, create, and delete Proxmox users
- **Manage tokens & ACLs** — create/delete API tokens, grant/revoke ACL roles per user

Navigation: **Enter** to select, **Esc** to go back, **Tab** to cycle between
VMs, Users, and Backups views, **Q** or **Ctrl+C** to quit.

Key bindings use plain letters for resource actions (lowercase for safe
actions, uppercase for destructive) and **Alt/Option+key** for
snapshot, backup, disk, and tag actions (`Alt+z` to resize a disk,
`Alt+m` to move a disk to a different storage, `Alt+t` to manage tags —
picks from tags already in use across the instance, or lets you insert a
new one). See the on-screen hints for all available shortcuts.

## Instance Management

Instances are stored in `~/.pxve.yaml`. Authentication supports both API
tokens (recommended) and username/password.

```sh
pxve instance add     <name> --url <url> --token-id '<id>' --token-secret <secret>
pxve instance add     <name> --url <url> --username root@pam --password <pass>
pxve instance list
pxve instance use     <name>
pxve instance show    [name]
pxve instance remove  <name>
pxve instance discover [subnet...]
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
pxve vm | ct  list                              [--node <node>]
pxve vm | ct  start    <id>                     [--node <node>]
pxve vm | ct  stop     <id>                     [--node <node>]
pxve vm | ct  shutdown <id>                     [--node <node>]
pxve vm | ct  reboot   <id>                     [--node <node>]
pxve vm | ct  info     <id>                     [--node <node>]
pxve vm | ct  clone    <id> <name>              [--node <node>] [--newid <id>]
pxve vm | ct  delete   <id>                     [--node <node>]
pxve vm | ct  template <id>                     [--node <node>] [--force]
pxve vm | ct  disk resize <id> <disk> <size>    [--node <node>]
pxve vm | ct  disk move   <id> [disk]           [--node <node>] --storage <s> [--delete=false] [--bwlimit <KiB/s>]
pxve vm       disk detach <id> <disk>           [--node <node>] [--delete] [--force]
pxve vm | ct  tag list   <id>                   [--node <node>]
pxve vm | ct  tag add    <id> <tag>             [--node <node>]
pxve vm | ct  tag remove <id> <tag>             [--node <node>]
pxve vm | ct  snapshot list     <id>            [--node <node>]
pxve vm | ct  snapshot create   <id> <name>     [--node <node>]
pxve vm | ct  snapshot rollback <id> <name>     [--node <node>]
pxve vm | ct  snapshot delete   <id> <name>     [--node <node>]
```

> **Notes:**
> * `vm shutdown` sends an ACPI signal (guest-initiated). `ct shutdown` sends an orderly shutdown request to the container runtime. Both are graceful, `stop` is always forceful.
> * Clones are always **full clones** (independent of the source).
> * `template` is **irreversible** — the VM or CT becomes read-only and can only be cloned afterwards. Use `--force` to skip the confirmation prompt.
> * `disk resize` grows a disk by a delta — specify the amount and unit (e.g. `10G`, `512M`); the `+` prefix is added automatically if omitted.
> * `disk move` moves a disk to a different storage; if the disk argument is omitted and only one moveable disk exists it is auto-selected, otherwise a prompt is shown. The source disk is deleted after the move by default (`--delete=false` to keep it). Supports live migration on running VMs.
> * `disk detach` (VM only) removes a disk from the VM config. Without `--delete` the data is preserved as an unused disk; with `--delete` it is permanently destroyed (confirmation required unless `--force`).
> * `tag` names may contain letters, digits, hyphens, underscores, and dots.

### Guest Agent (VMs only)

Interact with the QEMU guest agent running inside a VM. Requires the VM to be running
with `qemu-guest-agent` installed and active.

```
pxve vm agent exec         <vmid> -- <command> [args...]  [--node <node>] [--timeout <secs>] [--stdin <data>]
pxve vm agent osinfo       <vmid>                         [--node <node>]
pxve vm agent networks     <vmid>                         [--node <node>]
pxve vm agent set-password <vmid> --username <user>       [--node <node>] [--password <pw>]
```

> **Notes:**
> * `exec` runs a command inside the guest and prints stdout/stderr. Use `--` to separate pxve flags from the guest command. Default timeout is 30 seconds.
> * `osinfo` shows the guest OS name, version, kernel, and architecture.
> * `networks` lists guest network interfaces with MAC and IP addresses.
> * `set-password` sets a user's password inside the guest. If `--password` is omitted, you are prompted securely (input is hidden).
> * All agent commands support `--output json` for machine-readable output.

### Backups

```
pxve backup storages                       [--node <node>]
pxve backup list                           [--node <node>] [--storage <s>] [--vmid <id>]
pxve backup create  <vmid>                 [--node <node>] [--storage <s>] [--mode snapshot] [--compress zstd]
pxve backup delete  <volid>  --node <node> [--storage <s>]
pxve backup restore <volid>  --node <node> [--vmid <id>] [--name <name>] [--storage <s>]
pxve backup info    <volid>  --node <node>
```

- `storages` lists backup-capable storages with available/used/total space.
- `create` runs a vzdump backup. Node is auto-resolved from the VMID if omitted.
  Default mode is `snapshot`, default compression is `zstd`.
- `restore` recreates a VM or CT from a backup archive. VM vs CT is auto-detected
  from the volid. If `--vmid` is omitted, the next available ID is used. `--name`
  sets the VM name or CT hostname (defaults to the name embedded in the backup).
  `--storage` specifies where to place restored disks (must support `images` for VMs
  or `rootdir` for CTs, e.g. `local-lvm`).
- `info` extracts and displays the hardware configuration embedded in a backup.

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
