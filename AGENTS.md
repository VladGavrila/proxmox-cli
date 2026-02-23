# pxve — Agent Guide

A Go CLI for managing Proxmox VE clusters, built on top of
`github.com/luthermonson/go-proxmox v0.4.0`.

---

## Repository Layout

```
pxve/
├── main.go                          # Entry point — calls cli.Execute(version)
├── Makefile                         # build, tidy, clean targets
├── go.mod / go.sum
├── cli/                             # Cobra command layer (thin: parse → action → format)
│   ├── root.go                      # Root command, global flags, initClient(), handleErr(), --tui launch
│   ├── instance.go                  # instance add/remove/list/use/show/discover + verifyInstance()
│   ├── vm.go                        # vm list/start/stop/shutdown/reboot/info/clone/delete/snapshot/template/disk/tag
│   ├── container.go                 # ct list/start/stop/shutdown/reboot/info/clone/delete/snapshot/template/disk/tag
│   ├── backup.go                    # backup storages/list/create/delete/restore/info
│   ├── node.go                      # node list/info
│   ├── cluster.go                   # cluster status/resources/tasks
│   ├── user.go                      # user list/create/delete/password/grant/revoke/token
│   ├── access.go                    # acl list; role list
│   └── output.go                    # watchTask(), formatBytes(), formatUptime(), formatCPUPercent(), selectFromList(), normalizeDiskSize()
├── tui/                             # Bubble Tea interactive TUI (launched via pxve --tui)
│   ├── tui.go                       # appModel router, screen enum, LaunchTUI() entry point
│   ├── selector.go                  # Instance picker + inline add/remove/discover instance form
│   ├── discover.go                  # tea.Cmd wrapper around internal/discovery for the TUI
│   ├── list.go                      # VM + CT list table with auto-refresh, power/clone/delete
│   ├── detail.go                    # VM/CT detail: types, state machine, update()
│   ├── detail_cmds.go               # All tea.Cmd closures for detail screen
│   ├── detail_view.go               # Rendering — view(), tab views, overlays, formatUptime
│   ├── users.go                     # Proxmox user list + inline create/delete
│   ├── backups.go                   # Cluster-wide backup list + inline delete
│   ├── userdetail.go                # User detail: tokens + ACLs, create/delete/grant/revoke
│   └── styles.go                    # Shared lipgloss styles, headerLine(), CLISpinner
├── internal/
│   ├── config/config.go             # Load/Save ~/.pxve.yaml via yaml.v3 (NOT viper)
│   ├── client/client.go             # Build proxmox.Client from InstanceConfig
│   ├── errors/errors.go             # Handle() maps sentinel errors to friendly messages
│   ├── discovery/discovery.go       # Network scan for Proxmox instances (shared by CLI + TUI)
│   └── actions/                     # All Proxmox logic — shared by CLI and TUI
│       ├── vm.go                    # ListVMs, FindVM, Start/Stop/Shutdown/Reboot/Clone/Delete/Snapshots/ConvertToTemplate
│       ├── container.go             # ListContainers, FindContainer, Start/Stop/Shutdown/Reboot/Clone/Delete/Snapshots/ConvertToTemplate
│       ├── backup.go               # ListBackupStorages, ListBackups, CreateBackup, DeleteBackup, RestoreBackup, BackupConfig, NextID
│       ├── node.go                  # ListNodes, GetNode
│       ├── cluster.go               # GetCluster, ClusterResources, ClusterTasks
│       └── access.go                # ListUsers/Tokens/ACLs/Roles, Create/Delete, Grant/Revoke
└── dist/                            # Compiled binaries (git-ignored)
    ├── pxve-macos-arm64
    └── pxve-linux-amd64
```

---

## Key Architectural Rules

### CLI layer (`cli/`)
- Commands are **thin**: parse flags → call `internal/actions` → format output.
- Every `RunE` that talks to Proxmox calls `initClient(cmd)` first, then wraps errors
  with `handleErr(err)` (never return the raw error directly).
- Output respects the global `flagOutput` variable (`"table"` or `"json"`).
- Table output uses `text/tabwriter` — no third-party table library.
- **Every action call must be wrapped with a spinner** (see `cli/output.go`):
  ```go
  s := startSpinner("Loading...")   // or a more specific message
  result, err := actions.SomeAction(ctx, proxmoxClient, ...)
  s.Stop()                          // always stop before any output or return
  ```
  The spinner animates on stderr and is a no-op when stderr is not a terminal.
  Use `"Loading..."` for read commands and `"Connecting..."` for commands that
  return a task. Never call `s.Stop()` inside an `if err != nil` block only —
  it must run on both success and error paths.

### Config (`internal/config/config.go`)
- Config file: `~/.pxve.yaml`
- Uses `gopkg.in/yaml.v3` directly — **never use viper** (viper lowercases all map
  keys on load, which breaks case-sensitive instance names like `"pveHomeLab"`).
- `InstanceConfig` fields: `url`, `token-id`, `token-secret`, `username`, `password`,
  `verify-tls` (bool, default false — TLS is skipped unless explicitly set).
- `Config.Resolve(flagInstance)` priority: `--instance` flag → `PROXMOX_INSTANCE` env →
  `current-instance` in file.

### Client builder (`internal/client/client.go`)
- go-proxmox requires the base URL to end with `/api2/json`. The builder auto-appends
  it if missing, so users only need to provide `https://host:8006`.
- Supports both API token auth (`WithAPIToken`) and username/password
  (`WithCredentials`).
- Respects `insecure: true` via `tls.Config{InsecureSkipVerify: true}`.

### Error handling (`internal/errors/errors.go`)
- `Handle(instanceURL, err)` maps `proxmox.ErrNotAuthorized`, `proxmox.ErrNotFound`,
  `proxmox.ErrTimeout`, and connection errors to friendly messages.
- All `RunE` handlers end with `return handleErr(err)`, never `return err`.

### Actions layer (`internal/actions/`)
- Pure functions: take `ctx`, `*proxmox.Client`, and params; return results or errors.
- `FindVM` / `FindContainer` scan all nodes when `nodeName == ""`.
- `DeleteVMSnapshot` is implemented via a raw `c.Delete()` call because go-proxmox
  does not expose this method on `VirtualMachine`.
- `CloneVM` calls `c.Cluster().NextID()` to auto-assign the next available VMID when
  `newid == 0`.
- `ConvertVMToTemplate(ctx, c, vmid, nodeName)` — calls `vm.ConvertToTemplate(ctx)`
  and returns a `*proxmox.Task`. The VM must be stopped.
- `ConvertContainerToTemplate(ctx, c, ctid, nodeName)` — calls `ct.Template(ctx)`
  (returns `error`, no task). The container must be stopped.
- `ResizeVMDisk(ctx, c, vmid, nodeName, disk, size)` — calls `vm.ResizeDisk(ctx, disk, size)`.
  `disk` e.g. `"scsi0"`, `"virtio0"`; `size` must start with `'+'`, e.g. `"+10G"`. The CLI
  and TUI prepend `'+'` automatically, so callers of the action must pass it explicitly.
- `ResizeContainerDisk(ctx, c, ctid, nodeName, disk, size)` — uses a raw `c.Put()` to
  `/nodes/{node}/lxc/{ctid}/resize` instead of `ct.Resize()`, because go-proxmox sends
  `POST` but Proxmox requires `PUT`. `disk` e.g. `"rootfs"`, `"mp0"`; same `'+'` size format.
- `MoveVMDisk(ctx, c, vmid, nodeName, disk, storage, deleteAfter, bwlimit)` — moves a VM disk
  to a different storage. `deleteAfter=true` removes the source disk after the move.
  `bwlimit` in KiB/s (0 = unlimited). Supports live migration (moving disks on running VMs).
- `MoveContainerVolume(ctx, c, ctid, nodeName, volume, storage, deleteAfter, bwlimit)` — same
  for CT volumes (rootfs or mount points).
- `DetachVMDisk(ctx, c, vmid, nodeName, disk, deleteData)` — detaches a disk from a VM config.
  When `deleteData=false`, the disk is moved to an `unusedN` slot (data preserved). When
  `deleteData=true`, the disk data is permanently and irreversibly destroyed. Calls
  `vm.UnlinkDisk(ctx, disk, deleteData)`.
- **Tag actions** (`vm.go`, `container.go`, `cluster.go`): `VMTags(ctx, c, vmid, node)`,
  `AddVMTag(ctx, c, vmid, node, tag)`, `RemoveVMTag(ctx, c, vmid, node, tag)` — and
  matching `ContainerTags / AddContainerTag / RemoveContainerTag`. Proxmox stores tags as
  a semicolon-separated string (`"prod;web-server"`). The actions read/modify this string
  via `vm.Config()` and a raw PUT to the config endpoint. `splitTagsStr(s)` is an unexported
  helper that splits on `;` and filters empty strings. `AllInstanceTags(ctx, c)` (in
  `cluster.go`) scans all cluster resources and returns a sorted, deduplicated list of every
  tag in use across the instance — used by the TUI tag picker.
- **Backup actions** (`backup.go`): `ListBackupStorages` filters storages by `backup`
  content type. `ListRestoreStorages` filters by `images` (qemu) or `rootdir` (lxc)
  content type — these are distinct because backup-capable storages (e.g. `local`)
  often cannot hold VM disks or container rootfs. `RestoreBackup` uses raw
  `c.Post()` to `/nodes/{node}/qemu` (with `archive` + `name`) or
  `/nodes/{node}/lxc` (with `ostemplate` + `restore=1` + `hostname`).
  `NextID` is a convenience wrapper around `Cluster.NextID()`.

### Task watching (`cli/output.go`)
- `watchTask(ctx, w, task)` streams `task.Watch()` log lines and filters out blank
  lines and `"no content"` entries that Proxmox emits for fast operations.
- Falls back to `task.WaitFor(ctx, ...)` if Watch fails.

### Instance discovery (`internal/discovery/discovery.go`)

The `discovery` package is shared by both the CLI (`pxve instance discover`) and the TUI
selector screen (`d` key). Key entry points:

- `Scan(subnets []string) (*Result, error)` — TCP-scans port 8006 across all given /24 CIDRs
  (50 concurrent goroutines, 800 ms timeout per host), then HTTPS-verifies each open port
  against `/api2/json/version`. Returns `Result{Instances, Subnets}` where `Subnets` is the
  resolved list actually scanned (useful for "nothing found on X, Y" messages). If `subnets`
  is empty, `LocalSubnets()` is used automatically.
- `NormalizeSubnet(s string) (string, error)` — accepts an IP (`172.20.20.5`), a partial prefix
  (`172.20.20`), or a CIDR (`172.20.20.0/24`) and returns a canonical `/24` CIDR string.
- `LocalSubnets() ([]string, error)` — returns `/24` CIDRs for all active, non-loopback
  IPv4 interfaces on the local machine.

**Important**: Proxmox's `/api2/json/version` endpoint requires authentication (returns 401),
so the version field is not collected. Any HTTP response on port 8006 over HTTPS is treated
as confirmation the host is a Proxmox instance.

**Subnet scoping**: Discovery only finds instances reachable from the local machine's routing
table. If Proxmox lives on a different network (e.g. `172.20.20.0/24` while the local machine
is on `172.99.99.0/24`), that subnet must be specified explicitly — local auto-detection will
not find it.

### Instance add validation (`cli/instance.go`)
- Token-id format is validated with `tokenIDRegex` (`^[^@]+@[^!]+![^!]+$`) before
  any network call.
- `verifyInstance()` calls `/version` to confirm connectivity and auth, then calls
  `/access/domains` to verify the realm from the token-id exists on the server.
- The realm check is best-effort: if `/access/domains` fails, validation passes anyway.

### TLS behaviour
- TLS verification is **skipped by default**. The config field is `verify-tls` (bool),
  which defaults to `false` (skip). The Go zero value of bool maps naturally to the
  default behaviour — no field in the YAML = skip TLS.
- Use `--secure` on `instance add` to store `verify-tls: true` for that instance.
- The global `--secure` flag overrides the per-instance config for a single invocation.
- Old configs with `insecure: true` are safe: yaml.v3 ignores unknown fields, so the
  instance loads with `verify-tls` unset (false = skip TLS), which is correct.

---

## Adding a New Command

Follow this pattern — everything else already exists:

1. **Add an action function** in the appropriate `internal/actions/*.go` file:
   ```go
   func FooVM(ctx context.Context, c *proxmox.Client, vmid int, nodeName string) (*proxmox.Task, error) {
       vm, err := FindVM(ctx, c, vmid, nodeName)
       if err != nil {
           return nil, err
       }
       return vm.Foo(ctx)
   }
   ```

2. **Add a CLI command** in the matching `cli/*.go` file:
   ```go
   func vmFooCmd() *cobra.Command {
       var nodeName string
       cmd := &cobra.Command{
           Use:   "foo <vmid>",
           Short: "Foo a VM",
           Args:  cobra.ExactArgs(1),
           RunE: func(cmd *cobra.Command, args []string) error {
               vmid, err := strconv.Atoi(args[0])
               if err != nil {
                   return fmt.Errorf("invalid VMID %q", args[0])
               }
               if err := initClient(cmd); err != nil {
                   return err
               }
               ctx := context.Background()
               s := startSpinner("Connecting...")
               task, err := actions.FooVM(ctx, proxmoxClient, vmid, nodeName)
               s.Stop()
               if err != nil {
                   return handleErr(err)
               }
               fmt.Fprintf(cmd.OutOrStdout(), "Fooing VM %d...\n", vmid)
               if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
                   return handleErr(err)
               }
               fmt.Fprintf(cmd.OutOrStdout(), "VM %d fooed.\n", vmid)
               return nil
           },
       }
       cmd.Flags().StringVar(&nodeName, "node", "", "node name")
       return cmd
   }
   ```

3. **Register it** by adding `cmd.AddCommand(vmFooCmd())` inside `vmCmd()`.

4. **Build**: `make build` — produces `dist/pxve-macos-arm64` and
   `dist/pxve-linux-amd64`.

---

## TUI (`tui/`)

The interactive TUI is launched via `pxve --tui` (local flag on `rootCmd`).
It uses [Bubble Tea](https://github.com/charmbracelet/bubbletea) v0.25.0 with
Bubbles v0.18.0 and Lipgloss v0.9.1.

### Architecture

A single **router model** (`appModel` in `tui/tui.go`) owns six sub-models, one
per screen:

| Screen           | Model             | File             | Purpose                                          |
|------------------|-------------------|------------------|--------------------------------------------------|
| `screenSelector` | `selectorModel`   | `selector.go`    | Pick / add / remove / discover Proxmox instances |
| `screenList`     | `listModel`       | `list.go`        | Table of all VMs + CTs, power actions, clone, delete, template |
| `screenUsers`    | `usersModel`      | `users.go`       | Table of Proxmox users + create / delete          |
| `screenBackups`  | `backupsScreenModel` | `backups.go`  | Cluster-wide backup list + delete + restore        |
| `screenDetail`   | `detailModel`     | `detail.go` / `detail_cmds.go` / `detail_view.go` | VM/CT info (incl. primary disk storage), power, clone, delete, template, resize/move disks, tags, snapshots, backups |
| `screenUserDetail` | `userDetailModel` | `userdetail.go` | User info, token CRUD, ACL grant/revoke           |

### Navigation flow

```
Selector ──Enter──▸ List ──Enter──▸ Detail──▸ Esc
                     │ Tab
                     ▼
                   Users ──Enter──▸ UserDetail──▸ Esc
                     │ Tab
                     ▼
                   Backups
                     │ Tab
                     ▼
                    List
```

- **Esc** goes back one screen (or dismisses an active dialog/overlay).
- **Tab** cycles between List → Users → Backups → List.
- **Q** / **Ctrl+C** quits (Q is passed through when a text input is active).

### Key patterns

- **Sub-model typing**: Sub-models have typed `update(msg) (Model, tea.Cmd)`
  methods (not the `tea.Model` interface). The router delegates via a switch.
- **`withRebuiltTable()` value-receiver**: Used on resize — returns a copy of
  the model with a fresh `table.Model` sized to the new terminal dimensions.
- **Overlay modes**: Each screen that supports dialogs (selector, detail, users,
  userDetail) has a `mode` enum (`detailNormal`, `detailInputName`, etc.).
  The router checks the mode before consuming Esc/Q to avoid swallowing keys
  meant for a dialog.
- **Async messages**: API calls run in `tea.Cmd` functions and return typed
  messages (`resourcesFetchedMsg`, `detailLoadedMsg`, `actionResultMsg`, etc.).
  Stale responses are discarded by comparing `fetchID` timestamps.
- **Client caching**: `appModel.clientCache` stores connected `*proxmox.Client`
  instances keyed by instance name. `listCache` stores `listModel` state so
  switching back to a previously visited instance is instant.
- **Feedback + reload**: Mutating actions (snapshot create/delete, user
  create/delete, token create/delete, backup create/delete) emit a
  `reloadSnapshotsMsg` / `reloadBackupsMsg` / `usersActionMsg` /
  `userDetailActionMsg` that shows a status message and triggers an automatic
  list reload.
- **Selector key bindings**: On the instance selector screen, `a` opens the inline
  add-instance form, `d` opens a subnet input prompt and then scans for Proxmox
  instances (Enter with empty input scans local subnets; accepts partial prefix,
  full IP, or CIDR), `R` opens the remove-instance confirmation, and Enter connects
  to the selected instance. Discovery results appear in a table; selecting one
  pre-populates the add form with the discovered URL.
- **Key binding convention**: Resource actions use plain letter keys — non-destructive
  lowercase (`s` start, `c` clone), destructive uppercase (`S` stop, `U` shutdown,
  `R` reboot, `D` delete, `T` convert to template). Alt/Option+key for
  snapshot/backup/disk/tag actions: `Alt+s` new snapshot, `Alt+d` delete,
  `Alt+r` rollback/restore, `Alt+b` new backup, `Alt+z` resize disk, `Alt+m` move disk,
  `Alt+t` tag manager.
  `T`, `Alt+z`, and `Alt+m` are available on both the **list screen** and the **detail screen**.
  `Alt+t` is available on the **detail screen** only (tag list is shown in the detail stats line).
  macOS Option key sends Unicode (`ß`=s, `∂`=d, `®`=r, `∫`=b, `Ω`=z, `µ`=m, `†`=t, `ø`=o);
  both `alt+<key>` and the Unicode variant are matched in every case statement.
  Note: `Ω` (Option+z) ≠ `ø` (Option+o) — easy to confuse when adding new bindings.
- **List-level actions**: The list screen supports power actions, clone,
  delete-with-confirmation, convert-to-template, disk resize, and disk move — all
  operating on the highlighted resource. Clone: `listCloneInput` mode (2-field VMID +
  name form). Delete: `listConfirmDelete` mode (Enter/Esc). Template:
  `listConfirmTemplate` mode. Disk resize: `listResizeDisk` mode (2-field Disk + Size
  form; disk pre-filled from resource type — `scsi0` for VMs, `rootfs` for CTs; `+`
  prepended to size automatically). Disk move: `listSelectMoveDisk` → `listSelectMoveStorage`
  cursor-picker flow (mirrors the detail screen; skips the disk picker when only one disk
  exists, skips the storage picker when only one target exists).
  After any mutating action the list auto-refreshes via `actionResultMsg{needRefresh: true}`.
  `resourceDeletedMsg` is handled by the router (navigates back to list) and is only
  emitted by the detail screen's delete/template actions.
- **Detail tab bar**: The detail screen has two tabs (Snapshots and Backups),
  toggled with Tab. Each tab has its own data, table, loading state, and
  key bindings. Tab-specific keys (`Alt+s`/`Alt+d`/`Alt+r` for snapshots,
  `Alt+b`/`Alt+d`/`Alt+r` for backups) only fire when the corresponding
  tab is active.
- **Multi-step restore flow**: Backup restore is a 3-step wizard:
  (1) fetch next available ID → (2) `detailRestoreInputID` mode with two
  text inputs (VMID pre-populated, name optional) → (3) storage selection
  (`detailRestoreSelectStorage`) → execute. The VMID and name are carried
  through via `pendingRestoreID` / `pendingRestoreName` fields. The same
  flow exists on the top-level Backups screen (`backupsScreenRestoreInput`
  → `backupsScreenRestoreStorage`).
- **Clone flow**: Clone is a 2-step wizard on both list and detail screens:
  (1) press `c` → silently fetch next available ID (`cloneNextIDMsg`) →
  (2) show 2-field input form (VMID pre-filled, name pre-filled as
  `{original-name}-clone`) → Enter to confirm → execute clone with spinner.
  Uses a separate `cloneNextIDMsg` type (distinct from `nextIDLoadedMsg`
  used by restore) to avoid routing ambiguity. Both screens share the same
  message type defined in `detail.go`.
- **Storage selection**: Both backup creation and restore show a storage
  picker overlay when multiple candidate storages exist. If only one is
  available, the picker is skipped and it's used directly. Backup creation
  uses `ListBackupStorages` (content type `backup`); restore uses
  `ListRestoreStorages` (content type `images` for VMs, `rootdir` for CTs).
- **Clone behaviour**: Both VM and CT clones use **full clone** mode
  (`Full: 1`) rather than linked clones, ensuring the clone is fully
  independent of the source.

### Detail screen file layout (`tui/detail*.go`)

The detail screen logic is split across three files in the same package:

| File | Responsibility |
|---|---|
| `detail.go` | `detailMode` enum, all data/message types, `detailModel` struct, `newDetailModel`, `withRebuilt*`, `init`, `formatSnapTime`, the full `update()` state machine, `selectedSnapshot/BackupInfo` helpers |
| `detail_cmds.go` | All `tea.Cmd` closures: load (snapshots, backups, storages, nextID, primary disk), power, delete, clone, template, resize/move disk, snapshot CRUD, backup CRUD, tag CRUD |
| `detail_view.go` | `view()`, `renderTabBar()`, `viewSnapshotsTab()`, `viewBackupsTab()`, `viewOverlay()`, `formatUptime()` |

**`diskLocation` / `primaryDiskLoadedMsg` / `loadPrimaryDiskCmd`**: On detail screen init,
`loadPrimaryDiskCmd()` is batched alongside `loadSnapshotsCmd()` and `loadBackupsCmd()`.
It calls `FindVM`/`FindContainer`, sorts the disk keys, and returns the storage prefix of
the first non-cdrom disk (e.g. `local-lvm` from `local-lvm:vm-101-disk-0,size=32G`); for
CTs it parses `RootFS`. Errors are silently swallowed — the stats line shows `…` until
the value arrives, then the storage name. The field resets to `""` when the detail model
is re-constructed (e.g. navigating to a different resource).

When adding a new detail-screen feature: add the mode constant and message type to
`detail.go`, the API call to `detail_cmds.go`, and the overlay rendering to `detail_view.go`.

### Convert to Template

`[T]` converts the selected VM or CT to a Proxmox template. Available on both the
**list screen** and the **detail screen**. The action is irreversible (the resource
becomes read-only; it can only be cloned).

**List screen flow** (`listConfirmTemplate` mode):
1. Press `T` → shows confirmation overlay (`listConfirmTemplate` mode).
2. Press `Enter` → calls `listConvertToTemplateCmd()` → returns `actionResultMsg{needRefresh: true}`;
   the resource stays in the list (shown as a template after refresh).
3. If the resource is already a template (`r.Template == 1`), shows `"Already a template"` and does nothing.

**Detail screen flow** (`detailConfirmTemplate` mode):
1. Press `T` → sets `detailConfirmTemplate` mode (shows confirmation overlay).
2. Press `Enter` → calls `convertToTemplateCmd()` → dispatches `resourceDeletedMsg`
   on success (the resource disappears from Proxmox's VM list, so the router
   navigates back to the list screen).
3. If the resource is already a template (`r.Template == 1`), the key press shows
   `"Already a template"` and does nothing.

### Disk Resize

`[Alt+z]` (`Ω` on macOS) grows a disk on the selected VM or CT. Available on both
the **list screen** and the **detail screen**.

**Flow** (identical on both screens — mode `listResizeDisk` / `detailResizeDisk`):
1. Press `Alt+z` → disk field pre-filled (`scsi0` for VMs, `rootfs` for CTs); focus
   jumps straight to the size field.
2. Type the size delta (e.g. `10G`, `512M`) — the `+` prefix is added automatically.
3. `Tab`/`Shift+Tab` to move between Disk and Size fields if the default disk name
   needs to be changed.
4. `Enter` on the size field → calls `listResizeDiskCmd` / `resizeDiskCmd` →
   returns `actionResultMsg` (no navigation change; list auto-refreshes via
   `needRefresh: true`).

**Size normalisation**: both the CLI (`normalizeDiskSize`) and TUI silently prepend
`+` if the user omits it, so `10G` and `+10G` are both accepted.

**Library bug workaround**: `ResizeContainerDisk` uses a raw `c.Put()` call to
`/nodes/{node}/lxc/{ctid}/resize` because go-proxmox v0.4.0 uses `POST` for
`ct.Resize()`, but Proxmox requires `PUT`.

### Disk Move

`[Alt+m]` (`µ` on macOS) moves a disk or volume to a different storage on the selected
VM or CT. Available on both the **list screen** and the **detail screen**.

**Flow** (identical on both screens):
1. Press `Alt+m` → fetches disk list via `loadDisksCmd` / `listLoadDisksCmd`
   (calls `FindVM`/`FindContainer`, reads `MergeDisks()`/`MergeMps()`; CD-ROM media filtered out).
2. If one disk: skip picker, go straight to storage selection.
   If multiple disks: show `detailSelectMoveDisk` / `listSelectMoveDisk` cursor list → `Enter` to select.
3. Fetch move-target storages via `loadMoveStoragesCmd` / `listLoadMoveStoragesCmd`
   (reuses `ListRestoreStorages`; filters out the disk's current storage).
4. If one storage: execute immediately.
   If multiple: show `detailSelectMoveStorage` / `listSelectMoveStorage` cursor list → `Enter` to confirm.
5. Calls `MoveVMDisk` / `MoveContainerVolume` with `delete=true` (source disk removed after move).
   Returns `actionResultMsg` (no navigation change).

**Storage filter**: the source storage (parsed from the disk spec before the `:`) is
excluded from the target list so the user can't accidentally move a disk to where it
already lives.

### Tag Management

`[Alt+t]` (`†` on macOS) opens a tag browser overlay on the **detail screen**.

**Flow**:
1. Press `Alt+t` → `detailTagManage` mode — cursor list of the resource's current tags.
2. `↑`/`↓` (or `j`/`k`) to move the cursor; `d`/`backspace` removes the selected tag
   (fires `removeTagCmd` immediately, no extra confirmation).
3. `a` fetches all tags declared across the instance (`loadAllTagsCmd` →
   `actions.AllInstanceTags`) while showing a spinner. Three outcomes:
   - **Tags available**: switch to `detailTagSelect` — a picker showing every instance
     tag not already applied to this resource, plus a "New tag..." sentinel at the bottom.
   - **No tags anywhere** (or fetch error, or all instance tags already applied):
     skip the picker and go directly to `detailTagAdd` (text input).
4. In `detailTagSelect`: `↑`/`↓`/`j`/`k` to navigate; `Enter` on a tag adds it
   immediately; `Enter` on "New tag..." opens `detailTagAdd`; `Esc` returns to
   `detailTagManage`.
5. In `detailTagAdd`: type a new tag name → `Enter` to add (fires `addTagCmd`) or
   `Esc` to return to `detailTagManage`; invalid names (validated against
   `tagInputRegex`) show an error and stay in add mode.
6. `Esc` in `detailTagManage` closes the overlay (returns to `detailNormal`).

**Instance tag discovery**: `AllInstanceTags(ctx, c)` in `internal/actions/cluster.go`
calls `ClusterResources(ctx, c, "vm")`, splits every resource's semicolon-delimited
`Tags` field, deduplicates, and returns a sorted list. The picker filters this list to
exclude tags already applied to the current resource, so only actionable choices appear.

**Tag validation**: `tagInputRegex = ^[a-zA-Z0-9._-]+$` — letters, digits, hyphens,
underscores, and dots. Proxmox itself is more permissive, but this regex matches
common conventions and prevents accidental semicolons (which would corrupt the stored string).

**Tags display**: When `r.Tags != ""`, a `Tags: tag1, tag2` line is shown below the
stats line in the detail screen header. The list screen shows a `TAGS` column (width 15)
using `formatTagsCell()`: up to 2 tags joined by `, `, then `+N` for overflow.

**CLI**: `pxve vm tag list/add/remove <vmid>` and `pxve ct tag list/add/remove <ctid>`
call the same action layer functions.

### Shared styles (`tui/styles.go`)

All screens use the styles defined in `styles.go` (`StyleTitle`, `StyleError`,
`StyleSuccess`, etc.). The `headerLine()` helper places a title on the left
and a "Refreshed: ..." timestamp on the right. `CLISpinner` matches the
braille spinner used in the CLI output.

### Adding a new TUI screen

1. Create a new file `tui/newscreen.go` with a model struct, `update()`,
   `view()`, and an `init()` method that returns a `tea.Cmd`.
2. Add a new `screen` constant in `tui/tui.go`.
3. Add the model field to `appModel` and wire it into `Update()` (navigation
   transitions) and `View()`.
4. Handle `tea.WindowSizeMsg` in the router for the new screen.

---

## go-proxmox Library Notes

- Import path: `github.com/luthermonson/go-proxmox` (alias `proxmox` in all files)
- Key types: `proxmox.Client`, `proxmox.VirtualMachine`, `proxmox.Container`,
  `proxmox.Node`, `proxmox.Task`, `proxmox.ClusterResources`
- Sentinel errors: `proxmox.ErrNotAuthorized`, `proxmox.ErrNotFound`,
  `proxmox.ErrTimeout` — checked with `proxmox.IsNotAuthorized(err)` etc.
- `proxmox.ClusterResources` is `[]*proxmox.ClusterResource` — used by `ListVMs` and
  `ListContainers` because it comes pre-filtered by Proxmox's per-user ACLs.
- Some operations (e.g. VM snapshot delete) are missing from the library and must be
  called via `c.Get/Post/Delete(ctx, path, &result)` directly.
- `c.Get(path, ...)`: if `path` starts with `/`, it is appended to `baseURL`
  (`https://host:8006/api2/json`), producing the full URL automatically.

---

## Proxmox ACL Concepts

- ACL path for a VM: `/vms/<vmid>` (integer ID, not name)
- ACL path for a container: `/vms/<ctid>` (same namespace)
- Built-in roles: `PVEVMUser`, `PVEVMAdmin`, `PVEAdmin`, `Administrator`, `PVEAuditor`
- User IDs always include realm: `alice@pve`, `root@pam`
- API token IDs format: `user@realm!tokenname` (e.g. `root@pam!cli`)

---

## Build

```sh
make build          # macos-arm64 + linux-amd64 into dist/
make macos-arm      # macOS Apple Silicon only
make linux-amd64    # Linux x86-64 only
make tidy           # go mod tidy
make clean          # remove dist/
```

Binaries are fully static (`CGO_ENABLED=0`). The version string is hardcoded in
`cli/root.go` as `var version = "x.y.z"` and requires no build-time flags.

---

## Release

When the user requests release notes after completing an implementation:

1. **Ask the user what version number to release** — do not assume or auto-increment.
2. Once the user provides the version, update `var version` in `cli/root.go`:
   ```go
   var version = "<new-version>"
   ```
3. Confirm the build passes with `make build` and that `pxve --version` prints the new version.
