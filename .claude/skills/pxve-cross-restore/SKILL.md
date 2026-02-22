---
name: pxve-cross-restore
description: Backup a VM or container from one Proxmox instance and restore it to another. Use when the user wants to migrate, copy, or restore a resource across Proxmox instances.
allowed-tools: Bash(*)
---

# Cross-Instance Backup and Restore

Backup a VM or CT from one Proxmox instance and restore it to another using a shared storage (SMB/NFS) as the transfer medium.

## Context

- Binary: `./dist/pxve-macos-arm64` (or `pxve` if installed globally)
- Config: `~/.pxve.yaml` — lists named instances (e.g. `VMPX`, `pveCore`)
- Shared storage (e.g. `SMB`) must be mounted on **both** source and target instances
- Backup filenames encode the resource type:
  - CT → `vzdump-lxc-<id>-*.tar.zst`
  - VM → `vzdump-qemu-<id>-*.vma.zst`

---

## Pre-flight Check (ALWAYS run first)

Before doing anything else, verify that a shared storage exists on **both** instances. If it is missing from either side, **stop and inform the user** — the workflow cannot proceed without a common shared storage.

```bash
pxve backup storages -i <SOURCE_INSTANCE>
pxve backup storages -i <TARGET_INSTANCE>
```

Compare the two output lists and look for a storage name that appears in both **and** whose `TYPE` is a network/shared type (`cifs`, `nfs`, `rbd`, `glusterfs`, `pbs`). A `dir` type storage is node-local and will **not** work.

> **Note:** The `backup storages` output also shows the node name (the `NODE` column). Use this as `<TARGET_NODE>` in the restore step — no separate lookup needed.

**If no shared storage is found on the source:** bail out.
> "No shared backup storage is configured on `<SOURCE_INSTANCE>`. A shared storage (e.g. SMB/NFS) must be present on both instances. Ask the user to configure one in the Proxmox web UI under Datacenter → Storage before retrying."

**If no shared storage is found on the target:** bail out.
> "No shared backup storage is configured on `<TARGET_INSTANCE>`. A shared storage (e.g. SMB/NFS) must be present on both instances. Ask the user to configure one in the Proxmox web UI under Datacenter → Storage before retrying."

**If shared storage names differ between instances:** ask the user which storage to use — the backup must be written to the name visible on the source, and confirmed visible by the same name on the target.

Once a common shared storage is confirmed, use that storage name (e.g. `SMB`) for `--storage` in the backup step below.

---

## Container (LXC) Workflow

### 1. List containers on source

```bash
pxve ct list -i <SOURCE_INSTANCE>
```

### 2. Backup to shared storage

> **Critical:** use `--storage SMB`, NOT local. Local storage is not visible from the other instance.

```bash
pxve backup create <CTID> -i <SOURCE_INSTANCE> --storage SMB
```

### 3. Confirm backup is visible from target

```bash
pxve backup list -i <TARGET_INSTANCE> | grep "<CTID>"
```

Note the full VOLID — e.g. `SMB:backup/vzdump-lxc-1002-2026_02_21-20_58_39.tar.zst`

### 4. Find a storage that supports `rootdir` on the target

If existing CTs are present on the target, inspect one to confirm the storage name:

```bash
pxve ct info <EXISTING_CTID> -i <TARGET_INSTANCE> -o json | grep rootfs
# Output example: "rootfs": "local-lvm:vm-100-disk-0,size=8G"
# → storage name is "local-lvm"
```

**If no existing CTs are present**, skip this step and use `local-lvm` directly in the restore command — it is the Proxmox default for container rootfs. Adjust only if the restore fails with a storage error.

### 5. Restore

```bash
pxve backup restore "<VOLID>" \
  -i <TARGET_INSTANCE> \
  --node <TARGET_NODE> \
  --vmid <NEW_CTID> \
  --name <NEW_NAME> \
  --storage local-lvm        # must support rootdir
```

### 6. Verify

```bash
pxve ct info <NEW_CTID> -i <TARGET_INSTANCE>
```

### 7. Cleanup — ask the user

After confirming the restore succeeded, **ask the user**:

> "The backup `<VOLID>` is still on the shared storage. Do you want to delete it?"

If yes:

```bash
pxve backup delete "<VOLID>" -i <SOURCE_INSTANCE> --node <SOURCE_NODE>
```

If no, leave it and inform the user it remains on the shared storage.

### Full CT example

```bash
pxve backup create 1002 -i VMPX --storage SMB
pxve backup list -i pveCore | grep 1002
pxve backup restore "SMB:backup/vzdump-lxc-1002-2026_02_21-20_58_39.tar.zst" \
  -i pveCore --node pveCore --vmid 222 --name agentRestore --storage local-lvm
pxve ct info 222 -i pveCore
```

---

## Virtual Machine (QEMU) Workflow

### 1. List VMs on source

```bash
pxve vm list -i <SOURCE_INSTANCE>
```

### 2. Backup to shared storage

```bash
pxve backup create <VMID> -i <SOURCE_INSTANCE> --storage SMB
```

### 3. Confirm backup is visible from target

```bash
pxve backup list -i <TARGET_INSTANCE> | grep "<VMID>"
```

Note the full VOLID — e.g. `SMB:backup/vzdump-qemu-101-2026_02_21-20_51_08.vma.zst`

### 4. Find a storage that supports `images` on the target

If existing VMs are present on the target, inspect one to confirm the storage name:

```bash
pxve vm info <EXISTING_VMID> -i <TARGET_INSTANCE> -o json | grep -E '"scsi0|virtio0|ide0|sata0'
# Output example: "scsi0": "local-lvm:vm-101-disk-0,size=32G"
# → storage name is "local-lvm"
```

**If no existing VMs are present**, skip this step and use `local-lvm` directly in the restore command — it is the Proxmox default for VM disk images. Adjust only if the restore fails with a storage error.

### 5. Restore

```bash
pxve backup restore "<VOLID>" \
  -i <TARGET_INSTANCE> \
  --node <TARGET_NODE> \
  --vmid <NEW_VMID> \
  --name <NEW_NAME> \
  --storage local-lvm        # must support images
```

### 6. Verify

```bash
pxve vm info <NEW_VMID> -i <TARGET_INSTANCE>
```

### 7. Cleanup — ask the user

After confirming the restore succeeded, **ask the user**:

> "The backup `<VOLID>` is still on the shared storage. Do you want to delete it?"

If yes:

```bash
pxve backup delete "<VOLID>" -i <SOURCE_INSTANCE> --node <SOURCE_NODE>
```

If no, leave it and inform the user it remains on the shared storage.

### Full VM example

```bash
pxve backup create 101 -i pveCore --storage SMB
pxve backup list -i VMPX | grep 101
pxve backup restore "SMB:backup/vzdump-qemu-101-2026_02_21-20_51_08.vma.zst" \
  -i VMPX --node pve --vmid 300 --name migratedVM --storage local-lvm
pxve vm info 300 -i VMPX
```

---

## Common Errors

| Error | Resource | Cause | Fix |
|-------|----------|-------|-----|
| `storage 'local' does not support container directories` | CT | Wrong storage | Use `--storage local-lvm` (rootdir-capable) |
| `storage 'local' does not support disk images` | VM | Wrong storage | Use `--storage local-lvm` (images-capable) |
| Backup not found on target | CT/VM | Backup saved to local storage | Redo backup with `--storage SMB` |
| `storage 'SMB' does not support container directories` | CT | SMB/CIFS can't hold rootdir | Use `local-lvm` for `--storage` on restore |
