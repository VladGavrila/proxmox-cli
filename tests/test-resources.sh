#!/usr/bin/env bash
# Unified smoke tests for pxve vm / ct commands.
# Usage: ./tests/test-resources.sh --vm|--ct [binary]
#   binary defaults to ./dist/pxve-macos-arm64
#
# Offline tests (sections 1-2) need no Proxmox instance.
# CRUD tests (section 3) require TEST_ID=<existing-stopped-resource-id>.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/helpers.sh"

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------

MODE=""
case "${1:-}" in
  --vm)  MODE="vm"  ; shift ;;
  --ct)  MODE="ct"  ; shift ;;
  *)
    echo "Usage: $0 --vm|--ct [binary]"
    exit 1
    ;;
esac

resolve_bin "${1:-}"

# ---------------------------------------------------------------------------
# Mode variables
# ---------------------------------------------------------------------------

if [[ "$MODE" == "vm" ]]; then
  CMD="vm"
  ID_LABEL="VMID"
  id_label_lower="vmid"
  RESOURCE="VM"
  DEFAULT_DISK="scsi0"
  CONFIG_NAME_FLAG="--name"
else
  CMD="ct"
  ID_LABEL="CTID"
  id_label_lower="ctid"
  RESOURCE="container"
  DEFAULT_DISK="rootfs"
  CONFIG_NAME_FLAG="--hostname"
fi

# ---------------------------------------------------------------------------
# Mode-conditional helpers
# ---------------------------------------------------------------------------

# if_vm ASSERTION_FUNC NAME ... — only runs when MODE=vm
if_vm() {
  [[ "$MODE" == "vm" ]] && "$@"
}

# if_ct ASSERTION_FUNC NAME ... — only runs when MODE=ct
if_ct() {
  [[ "$MODE" == "ct" ]] && "$@"
}

echo "Running $CMD CLI tests against $BIN ..."
echo ""

# ===========================================================================
# Section 1: Help & flag presence (no network)
# ===========================================================================

echo "--- Section 1: Help & flag presence ---"

# Shared commands
assert_output_contains \
  "$CMD --help shows subcommands" \
  "Available Commands" \
  "$BIN" $CMD --help

assert_output_contains \
  "$CMD list --help" \
  "List" \
  "$BIN" $CMD list --help

assert_output_contains \
  "$CMD start --help contains <$id_label_lower>" \
  "<$id_label_lower>" \
  "$BIN" $CMD start --help

assert_output_contains \
  "$CMD stop --help contains <$id_label_lower>" \
  "<$id_label_lower>" \
  "$BIN" $CMD stop --help

assert_output_contains \
  "$CMD shutdown --help contains <$id_label_lower>" \
  "<$id_label_lower>" \
  "$BIN" $CMD shutdown --help

assert_output_contains \
  "$CMD reboot --help contains <$id_label_lower>" \
  "<$id_label_lower>" \
  "$BIN" $CMD reboot --help

assert_output_contains \
  "$CMD info --help contains <$id_label_lower>" \
  "<$id_label_lower>" \
  "$BIN" $CMD info --help

assert_output_contains \
  "$CMD config --help contains $CONFIG_NAME_FLAG" \
  "$CONFIG_NAME_FLAG" \
  "$BIN" $CMD config --help

assert_output_contains \
  "$CMD clone --help contains --newid" \
  "--newid" \
  "$BIN" $CMD clone --help

assert_output_contains \
  "$CMD delete --help contains <$id_label_lower>" \
  "<$id_label_lower>" \
  "$BIN" $CMD delete --help

assert_output_contains \
  "$CMD template --help contains --force" \
  "--force" \
  "$BIN" $CMD template --help

assert_output_contains \
  "$CMD snapshot --help shows subcommands" \
  "Available Commands" \
  "$BIN" $CMD snapshot --help

assert_output_contains \
  "$CMD snapshot list --help" \
  "<$id_label_lower>" \
  "$BIN" $CMD snapshot list --help

assert_output_contains \
  "$CMD snapshot create --help" \
  "<$id_label_lower>" \
  "$BIN" $CMD snapshot create --help

assert_output_contains \
  "$CMD snapshot delete --help" \
  "<$id_label_lower>" \
  "$BIN" $CMD snapshot delete --help

assert_output_contains \
  "$CMD snapshot rollback --help" \
  "<$id_label_lower>" \
  "$BIN" $CMD snapshot rollback --help

assert_output_contains \
  "$CMD disk --help shows subcommands" \
  "Available Commands" \
  "$BIN" $CMD disk --help

assert_output_contains \
  "$CMD disk resize --help contains $DEFAULT_DISK" \
  "$DEFAULT_DISK" \
  "$BIN" $CMD disk resize --help

assert_output_contains \
  "$CMD disk move --help contains --storage" \
  "--storage" \
  "$BIN" $CMD disk move --help

assert_output_contains \
  "$CMD tag --help shows subcommands" \
  "Available Commands" \
  "$BIN" $CMD tag --help

assert_output_contains \
  "$CMD tag list --help" \
  "<$id_label_lower>" \
  "$BIN" $CMD tag list --help

assert_output_contains \
  "$CMD tag add --help" \
  "<$id_label_lower>" \
  "$BIN" $CMD tag add --help

assert_output_contains \
  "$CMD tag remove --help" \
  "<$id_label_lower>" \
  "$BIN" $CMD tag remove --help

# VM-only help checks
if_vm assert_output_contains \
  "vm agent --help shows subcommands" \
  "Available Commands" \
  "$BIN" vm agent --help

if_vm assert_output_contains \
  "vm agent exec --help contains --timeout" \
  "--timeout" \
  "$BIN" vm agent exec --help

if_vm assert_output_contains \
  "vm agent osinfo --help" \
  "<vmid>" \
  "$BIN" vm agent osinfo --help

if_vm assert_output_contains \
  "vm agent networks --help" \
  "<vmid>" \
  "$BIN" vm agent networks --help

if_vm assert_output_contains \
  "vm agent set-password --help contains --username" \
  "--username" \
  "$BIN" vm agent set-password --help

if_vm assert_output_contains \
  "vm disk detach --help contains --delete" \
  "--delete" \
  "$BIN" vm disk detach --help

if_vm assert_output_contains \
  "vm config --help contains --sockets" \
  "--sockets" \
  "$BIN" vm config --help

if_vm assert_output_contains \
  "vm config --help contains --balloon" \
  "--balloon" \
  "$BIN" vm config --help

if_vm assert_output_contains \
  "vm config --help contains --cpu" \
  "--cpu" \
  "$BIN" vm config --help

# CT-only help checks
if_ct assert_output_contains \
  "container alias works" \
  "Available Commands" \
  "$BIN" container --help

if_ct assert_output_contains \
  "ct config --help contains --swap" \
  "--swap" \
  "$BIN" ct config --help

# ===========================================================================
# Section 2: Argument validation (no network)
# ===========================================================================

echo "--- Section 2: Argument validation ---"

# Shared — missing args
assert_fail "$CMD start (no args) fails"           "$BIN" $CMD start
assert_fail "$CMD stop (no args) fails"             "$BIN" $CMD stop
assert_fail "$CMD shutdown (no args) fails"         "$BIN" $CMD shutdown
assert_fail "$CMD reboot (no args) fails"           "$BIN" $CMD reboot
assert_fail "$CMD info (no args) fails"             "$BIN" $CMD info
assert_fail "$CMD config (no args) fails"           "$BIN" $CMD config
assert_fail "$CMD clone (no args) fails"            "$BIN" $CMD clone
assert_fail "$CMD clone (1 arg) fails"              "$BIN" $CMD clone 100
assert_fail "$CMD delete (no args) fails"           "$BIN" $CMD delete
assert_fail "$CMD template (no args) fails"         "$BIN" $CMD template
assert_fail "$CMD snapshot list (no args) fails"    "$BIN" $CMD snapshot list
assert_fail "$CMD snapshot create (no args) fails"  "$BIN" $CMD snapshot create
assert_fail "$CMD snapshot create (1 arg) fails"    "$BIN" $CMD snapshot create 100
assert_fail "$CMD snapshot delete (no args) fails"  "$BIN" $CMD snapshot delete
assert_fail "$CMD snapshot delete (1 arg) fails"    "$BIN" $CMD snapshot delete 100
assert_fail "$CMD snapshot rollback (no args) fails"  "$BIN" $CMD snapshot rollback
assert_fail "$CMD snapshot rollback (1 arg) fails"    "$BIN" $CMD snapshot rollback 100
assert_fail "$CMD disk resize (no args) fails"      "$BIN" $CMD disk resize
assert_fail "$CMD disk resize (1 arg) fails"        "$BIN" $CMD disk resize 100
assert_fail "$CMD disk resize (2 args) fails"       "$BIN" $CMD disk resize 100 scsi0
assert_fail "$CMD tag list (no args) fails"         "$BIN" $CMD tag list
assert_fail "$CMD tag add (no args) fails"          "$BIN" $CMD tag add
assert_fail "$CMD tag add (1 arg) fails"            "$BIN" $CMD tag add 100
assert_fail "$CMD tag remove (no args) fails"       "$BIN" $CMD tag remove
assert_fail "$CMD tag remove (1 arg) fails"         "$BIN" $CMD tag remove 100

# Invalid ID
assert_stderr_contains \
  "$CMD start abc → invalid $ID_LABEL" \
  "invalid $ID_LABEL" \
  "$BIN" $CMD start abc

# VM-only validation
if_vm assert_fail "vm agent exec (no args) fails"          "$BIN" vm agent exec
if_vm assert_fail "vm agent osinfo (no args) fails"        "$BIN" vm agent osinfo
if_vm assert_fail "vm agent networks (no args) fails"      "$BIN" vm agent networks
if_vm assert_fail "vm agent set-password (no args) fails"  "$BIN" vm agent set-password
if_vm assert_fail "vm disk detach (no args) fails"         "$BIN" vm disk detach
if_vm assert_fail "vm disk detach (1 arg) fails"           "$BIN" vm disk detach 100

# ===========================================================================
# Section 3: CRUD lifecycle (requires Proxmox + TEST_ID)
# ===========================================================================

if [[ -z "${TEST_ID:-}" ]]; then
  echo ""
  echo "--- Section 3: CRUD lifecycle SKIPPED (TEST_ID not set) ---"
  echo "  Set TEST_ID=<existing-stopped-${id_label_lower}> to run CRUD tests."
  echo ""
else
  echo "--- Section 3: CRUD lifecycle (TEST_ID=$TEST_ID) ---"

  # List
  assert_output_contains \
    "$CMD list contains $ID_LABEL header" \
    "$ID_LABEL" \
    "$BIN" $CMD list

  # List JSON
  assert_output_contains \
    "$CMD list -o json contains type" \
    '"type"' \
    "$BIN" $CMD list -o json

  # Info
  assert_output_contains \
    "$CMD info $TEST_ID contains Status" \
    "Status:" \
    "$BIN" $CMD info "$TEST_ID"

  # Config (read)
  assert_output_contains \
    "$CMD config $TEST_ID contains Memory" \
    "Memory:" \
    "$BIN" $CMD config "$TEST_ID"

  # Tag add
  assert_output_contains \
    "$CMD tag add smoke-test-tag" \
    "added" \
    "$BIN" $CMD tag add "$TEST_ID" smoke-test-tag

  # Tag list
  assert_output_contains \
    "$CMD tag list contains smoke-test-tag" \
    "smoke-test-tag" \
    "$BIN" $CMD tag list "$TEST_ID"

  # Tag remove
  assert_output_contains \
    "$CMD tag remove smoke-test-tag" \
    "removed" \
    "$BIN" $CMD tag remove "$TEST_ID" smoke-test-tag

  # Snapshot create
  assert_output_contains \
    "$CMD snapshot create smoke-snap" \
    "created" \
    "$BIN" $CMD snapshot create "$TEST_ID" smoke-snap

  # Snapshot list
  assert_output_contains \
    "$CMD snapshot list contains smoke-snap" \
    "smoke-snap" \
    "$BIN" $CMD snapshot list "$TEST_ID"

  # Snapshot rollback
  assert \
    "$CMD snapshot rollback smoke-snap" \
    "$BIN" $CMD snapshot rollback "$TEST_ID" smoke-snap

  # Snapshot delete
  assert_output_contains \
    "$CMD snapshot delete smoke-snap" \
    "deleted" \
    "$BIN" $CMD snapshot delete "$TEST_ID" smoke-snap

  # Snapshot verify gone
  assert \
    "$CMD snapshot list succeeds after delete" \
    "$BIN" $CMD snapshot list "$TEST_ID"
fi

# ===========================================================================
# Section 4: Existing commands still work
# ===========================================================================

echo "--- Section 4: Existing commands still work ---"

assert_output_contains \
  "user list still works" \
  "USERID" \
  "$BIN" user list

assert_output_contains \
  "group list still works" \
  "GROUPID" \
  "$BIN" group list

# ===========================================================================
# Report
# ===========================================================================

print_report
