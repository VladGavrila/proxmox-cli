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
│   ├── instance.go                  # instance add/remove/list/use/show + verifyInstance()
│   ├── vm.go                        # vm list/start/stop/shutdown/reboot/info/clone/delete/snapshot
│   ├── container.go                 # ct list/start/stop/shutdown/reboot/info/clone/delete/snapshot
│   ├── node.go                      # node list/info
│   ├── cluster.go                   # cluster status/resources/tasks
│   ├── user.go                      # user list/create/delete/password/grant/revoke/token
│   ├── access.go                    # acl list; role list
│   └── output.go                    # watchTask(), formatBytes(), formatUptime(), formatCPUPercent()
├── tui/                             # Bubble Tea interactive TUI (launched via pxve --tui)
│   ├── tui.go                       # appModel router, screen enum, LaunchTUI() entry point
│   ├── selector.go                  # Instance picker + inline add/remove instance form
│   ├── list.go                      # VM + CT list table with auto-refresh
│   ├── detail.go                    # VM/CT detail: info, power actions, snapshot CRUD
│   ├── users.go                     # Proxmox user list + inline create/delete
│   ├── userdetail.go                # User detail: tokens + ACLs, create/delete/grant/revoke
│   └── styles.go                    # Shared lipgloss styles, headerLine(), CLISpinner
├── internal/
│   ├── config/config.go             # Load/Save ~/.pxve.yaml via yaml.v3 (NOT viper)
│   ├── client/client.go             # Build proxmox.Client from InstanceConfig
│   ├── errors/errors.go             # Handle() maps sentinel errors to friendly messages
│   └── actions/                     # All Proxmox logic — shared by CLI and TUI
│       ├── vm.go                    # ListVMs, FindVM, Start/Stop/Shutdown/Reboot/Clone/Delete/Snapshots
│       ├── container.go             # ListContainers, FindContainer, Start/Stop/Shutdown/Reboot/Clone/Delete/Snapshots
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

### Task watching (`cli/output.go`)
- `watchTask(ctx, w, task)` streams `task.Watch()` log lines and filters out blank
  lines and `"no content"` entries that Proxmox emits for fast operations.
- Falls back to `task.WaitFor(ctx, ...)` if Watch fails.

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

A single **router model** (`appModel` in `tui/tui.go`) owns five sub-models, one
per screen:

| Screen           | Model             | File             | Purpose                                          |
|------------------|-------------------|------------------|--------------------------------------------------|
| `screenSelector` | `selectorModel`   | `selector.go`    | Pick / add / remove Proxmox instances from config |
| `screenList`     | `listModel`       | `list.go`        | Table of all VMs + CTs for the connected instance |
| `screenUsers`    | `usersModel`      | `users.go`       | Table of Proxmox users + create / delete          |
| `screenDetail`   | `detailModel`     | `detail.go`      | VM/CT info, power actions, snapshot CRUD           |
| `screenUserDetail` | `userDetailModel` | `userdetail.go` | User info, token CRUD, ACL grant/revoke           |

### Navigation flow

```
Selector ──Enter──▸ List ──Enter──▸ Detail
                     │ Tab             │
                     ▼                 │ Esc
                   Users ──Enter──▸ UserDetail
                     │                 │
                     Esc               Esc
                     ▼                 ▼
                  Selector           Users
```

- **Esc** goes back one screen (or dismisses an active dialog/overlay).
- **Tab** toggles between the List and Users screens.
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
  create/delete, token create/delete) emit a `reloadSnapshotsMsg` /
  `usersActionMsg` / `userDetailActionMsg` that shows a status message and
  triggers an automatic list reload.

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

Binaries are fully static (`CGO_ENABLED=0`). Version is injected via
`-X main.version=$(git describe --tags --always --dirty)`, defaulting to `dev`.
