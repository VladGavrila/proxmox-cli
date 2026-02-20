# proxmox-cli

A command-line tool for managing Proxmox VE infrastructure — virtual machines,
containers, nodes, users, and access control.

## Features

- **VMs & containers** — list, start, stop, reboot, shutdown, clone, delete, snapshots
- **Nodes & cluster** — status, resources, running tasks
- **Users & tokens** — create, delete, password, API token management
- **ACLs** — grant and revoke roles on VMs, containers, or arbitrary paths
- **Multi-instance** — manage multiple Proxmox servers with named profiles
- **Output formats** — human-readable tables or `--output json`

## Quick Start

```sh
# Add a Proxmox instance (verifies connectivity on add)
proxmox-cli instance add home-lab \
  --url https://192.168.1.10:8006 \
  --token-id root@pam!cli \
  --token-secret <secret>

# Set it as the default
proxmox-cli instance use home-lab

# List VMs
proxmox-cli vm list

# Start a VM
proxmox-cli vm start 101

# Take a snapshot
proxmox-cli vm snapshot create 101 before-upgrade

# Create a user and grant VM access
proxmox-cli user create alice@pve --password secret
proxmox-cli user grant alice@pve --vmid 101 --role PVEVMUser
```

## Instance Management

Instances are stored in `~/.proxmox-cli.yaml`. Authentication supports both API
tokens (recommended) and username/password.

```sh
proxmox-cli instance add <name> --url <url> --token-id '<id>' --token-secret <secret>
proxmox-cli instance add <name> --url <url> --username root@pam --password <pass>
proxmox-cli instance list
proxmox-cli instance use <name>
proxmox-cli instance show [name]
proxmox-cli instance remove <name>
```

TLS certificate verification is **skipped by default** (most Proxmox nodes use
self-signed certs). Add `--secure` to enforce certificate verification for instances
with a valid certificate chain.

For one-off commands without saving an instance:

```sh
proxmox-cli vm list --url https://host:8006 --token-id root@pam!cli --token-secret <secret>
```

## Building

Requires Go 1.21+.

```sh
make build          # builds both targets into dist/
make darwin-arm     # dist/proxmox-cli-darwin-arm64
make linux-amd64    # dist/proxmox-cli-linux-amd64
make clean          # removes dist/
```

Binaries are fully static (`CGO_ENABLED=0`) with no runtime dependencies.

## Command Reference

```
proxmox-cli vm list [--node <node>]
proxmox-cli vm start <vmid> [--node <node>]
proxmox-cli vm stop <vmid>
proxmox-cli vm shutdown <vmid>        # graceful ACPI shutdown
proxmox-cli vm reboot <vmid>
proxmox-cli vm info <vmid>
proxmox-cli vm clone <vmid> <name> [--newid <id>]
proxmox-cli vm delete <vmid>
proxmox-cli vm snapshot list <vmid>
proxmox-cli vm snapshot create <vmid> <name>
proxmox-cli vm snapshot rollback <vmid> <name>
proxmox-cli vm snapshot delete <vmid> <name>

proxmox-cli ct list [--node <node>]
proxmox-cli ct start <ctid>
proxmox-cli ct stop <ctid>
proxmox-cli ct reboot <ctid>
proxmox-cli ct info <ctid>
proxmox-cli ct snapshot list <ctid>
proxmox-cli ct snapshot create <ctid> <name>
proxmox-cli ct snapshot rollback <ctid> <name>
proxmox-cli ct snapshot delete <ctid> <name>

proxmox-cli node list
proxmox-cli node info <node>

proxmox-cli cluster status
proxmox-cli cluster resources
proxmox-cli cluster tasks

proxmox-cli user list
proxmox-cli user create <userid> [--password <pw>] [--email <e>] [--firstname <f>] [--lastname <l>]
proxmox-cli user delete <userid>
proxmox-cli user password <userid> --password <new>
proxmox-cli user grant <userid> --vmid <id>[,<id>] --role <role>
proxmox-cli user grant <userid> --path /storage/local --role PVEDatastoreUser
proxmox-cli user revoke <userid> --vmid <id> --role <role>
proxmox-cli user token list <userid>
proxmox-cli user token create <userid> <tokenid>
proxmox-cli user token delete <userid> <tokenid>

proxmox-cli acl list [--user <userid>]
proxmox-cli role list
```

Global flags available on every command:

```
-i, --instance <name>    use a named instance from config
    --output json         output as JSON instead of table
    --secure              enforce TLS certificate verification (default: skip)
```
