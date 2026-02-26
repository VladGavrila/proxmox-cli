#!/usr/bin/env bash
# Quick smoke tests for the pxve backup command.
# Usage: ./tests/test-backup.sh [binary]
#   binary defaults to ./dist/pxve-macos-arm64
#
# Environment variables for CRUD lifecycle (Section 3):
#   TEST_ID              Existing stopped VMID to back up (required)
#   TEST_NODE            Node hosting that VMID (required)
#   TEST_STORAGE         Backup storage name for create (optional)
#   TEST_RESTORE_STORAGE Restore-target storage e.g. local-lvm (optional; enables restore test)

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/helpers.sh"

resolve_bin "${1:-}"

echo "Running backup CLI tests against $BIN ..."
echo ""

# ===========================================================================
# Section 1: Help & flag presence (no network required)
# ===========================================================================

assert_output_contains \
  "backup --help shows subcommands" \
  "Available Commands" \
  "$BIN" backup --help

assert_output_contains \
  "backup storages --help" \
  "backup-capable" \
  "$BIN" backup storages --help

assert_output_contains \
  "backup storages --help shows --node" \
  "--node" \
  "$BIN" backup storages --help

assert_output_contains \
  "backup list --help" \
  "List backups" \
  "$BIN" backup list --help

assert_output_contains \
  "backup list --help shows --vmid" \
  "--vmid" \
  "$BIN" backup list --help

assert_output_contains \
  "backup list --help shows --storage" \
  "--storage" \
  "$BIN" backup list --help

assert_output_contains \
  "backup list --help shows --node" \
  "--node" \
  "$BIN" backup list --help

assert_output_contains \
  "backup create --help shows <vmid>" \
  "<vmid>" \
  "$BIN" backup create --help

assert_output_contains \
  "backup create --help shows --mode" \
  "--mode" \
  "$BIN" backup create --help

assert_output_contains \
  "backup create --help shows --compress" \
  "--compress" \
  "$BIN" backup create --help

assert_output_contains \
  "backup create --help shows --storage" \
  "--storage" \
  "$BIN" backup create --help

assert_output_contains \
  "backup delete --help shows <volid>" \
  "<volid>" \
  "$BIN" backup delete --help

assert_output_contains \
  "backup delete --help shows --node" \
  "--node" \
  "$BIN" backup delete --help

assert_output_contains \
  "backup restore --help shows <volid>" \
  "<volid>" \
  "$BIN" backup restore --help

assert_output_contains \
  "backup restore --help shows --node" \
  "--node" \
  "$BIN" backup restore --help

assert_output_contains \
  "backup restore --help shows --vmid" \
  "--vmid" \
  "$BIN" backup restore --help

assert_output_contains \
  "backup restore --help shows --name" \
  "--name" \
  "$BIN" backup restore --help

assert_output_contains \
  "backup restore --help shows --storage" \
  "--storage" \
  "$BIN" backup restore --help

assert_output_contains \
  "backup info --help shows <volid>" \
  "<volid>" \
  "$BIN" backup info --help

assert_output_contains \
  "backup info --help shows --node" \
  "--node" \
  "$BIN" backup info --help

# ===========================================================================
# Section 2: Argument validation (no network required)
# ===========================================================================

assert_fail \
  "backup create (no args) fails" \
  "$BIN" backup create

assert_fail \
  "backup delete (no args) fails" \
  "$BIN" backup delete

assert_fail \
  "backup restore (no args) fails" \
  "$BIN" backup restore

assert_fail \
  "backup info (no args) fails" \
  "$BIN" backup info

assert_stderr_contains \
  "backup create abc → invalid VMID" \
  "invalid VMID" \
  "$BIN" backup create notanumber

# --node is MarkFlagRequired on delete/restore/info, so Cobra rejects before network.
assert_fail \
  "backup delete missing --node fails" \
  "$BIN" backup delete "some:backup/vzdump-qemu-999-2025_01_01-00_00_00.vma.zst"

assert_fail \
  "backup restore missing --node fails" \
  "$BIN" backup restore "some:backup/vzdump-qemu-999-2025_01_01-00_00_00.vma.zst"

assert_fail \
  "backup info missing --node fails" \
  "$BIN" backup info "some:backup/vzdump-qemu-999-2025_01_01-00_00_00.vma.zst"

# ===========================================================================
# Section 3: CRUD lifecycle (requires Proxmox + TEST_ID + TEST_NODE)
# ===========================================================================

if [[ -z "${TEST_ID:-}" || -z "${TEST_NODE:-}" ]]; then
  echo ""
  echo "Skipping CRUD tests. Set TEST_ID=<existing-stopped-vmid> and TEST_NODE=<node> to run them."
else
  echo ""
  echo "--- CRUD lifecycle (VMID=$TEST_ID  NODE=$TEST_NODE) ---"

  # 1. Storages
  assert_output_contains \
    "backup storages" \
    "NAME" \
    "$BIN" backup storages --node "$TEST_NODE"

  # 2. List (baseline)
  assert \
    "backup list (baseline)" \
    "$BIN" backup list --node "$TEST_NODE" --vmid "$TEST_ID"

  # 3. Create
  CREATE_ARGS=("$BIN" backup create "$TEST_ID" --node "$TEST_NODE" --mode snapshot --compress zstd)
  if [[ -n "${TEST_STORAGE:-}" ]]; then
    CREATE_ARGS+=(--storage "$TEST_STORAGE")
  fi
  assert_output_contains \
    "backup create" \
    "Backup of VMID $TEST_ID completed." \
    "${CREATE_ARGS[@]}"

  # 4. Capture volid from the newest backup
  BACKUP_VOLID=$("$BIN" backup list --vmid "$TEST_ID" --node "$TEST_NODE" 2>&1 | awk 'NR==2{print $1}')
  if [[ -z "$BACKUP_VOLID" ]]; then
    echo "  WARN: could not capture backup volid — skipping remaining CRUD tests."
  else
    echo "  Captured VOLID: $BACKUP_VOLID"

    # 5. List (verify volid present)
    assert_output_contains \
      "backup list contains new volid" \
      "$BACKUP_VOLID" \
      "$BIN" backup list --node "$TEST_NODE" --vmid "$TEST_ID"

    # 6. List JSON
    assert_output_contains \
      "backup list JSON contains Volid" \
      "Volid" \
      "$BIN" backup list --node "$TEST_NODE" --vmid "$TEST_ID" -o json

    # 7. Info
    assert \
      "backup info" \
      "$BIN" backup info "$BACKUP_VOLID" --node "$TEST_NODE"

    # 8. Restore (only if TEST_RESTORE_STORAGE is set)
    if [[ -z "${TEST_RESTORE_STORAGE:-}" ]]; then
      echo "  Skipping restore test. Set TEST_RESTORE_STORAGE=<storage> to enable."
    else
      echo "  --- Restore sub-section (storage=$TEST_RESTORE_STORAGE) ---"
      RESTORED_ID=""
      _restore_out=$("$BIN" backup restore "$BACKUP_VOLID" --node "$TEST_NODE" --storage "$TEST_RESTORE_STORAGE" 2>&1) || true
      if echo "$_restore_out" | grep -qF "Restore complete"; then
        RESULTS+=("PASS  backup restore")
        ((PASS++))
      else
        RESULTS+=("FAIL  backup restore  (expected 'Restore complete' in output)")
        ((FAIL++))
      fi

      # Capture restored VMID
      RESTORED_ID=$(echo "$_restore_out" | grep -o 'VMID [0-9]*' | tail -1 | awk '{print $2}')

      if [[ -n "$RESTORED_ID" ]]; then
        echo "  Restored VMID: $RESTORED_ID"

        # Verify restored resource exists
        assert \
          "restored resource exists (vm info $RESTORED_ID)" \
          "$BIN" vm info "$RESTORED_ID"

        # Cleanup: delete restored resource
        # Detect type from volid
        if echo "$BACKUP_VOLID" | grep -qF "vzdump-lxc-"; then
          _del_cmd="ct"
        else
          _del_cmd="vm"
        fi
        echo "  Cleaning up: $_del_cmd delete $RESTORED_ID"
        # Stop first in case it's running, then delete
        "$BIN" "$_del_cmd" stop "$RESTORED_ID" >/dev/null 2>&1 || true
        "$BIN" "$_del_cmd" delete "$RESTORED_ID" >/dev/null 2>&1 || true
      else
        echo "  WARN: could not parse restored VMID from output."
      fi
    fi

    # 9. Delete backup (run once, inline assertions)
    _del_out=$("$BIN" backup delete "$BACKUP_VOLID" --node "$TEST_NODE" 2>&1) || true
    _del_ok=true
    if echo "$_del_out" | grep -qF "Deleting $BACKUP_VOLID"; then
      RESULTS+=("PASS  backup delete shows Deleting message")
      ((PASS++))
    else
      RESULTS+=("FAIL  backup delete shows Deleting message  (expected 'Deleting $BACKUP_VOLID')")
      ((FAIL++))
      _del_ok=false
    fi
    if echo "$_del_out" | grep -qF "Backup deleted."; then
      RESULTS+=("PASS  backup delete shows completion message")
      ((PASS++))
    else
      RESULTS+=("FAIL  backup delete shows completion message  (expected 'Backup deleted.')")
      ((FAIL++))
      _del_ok=false
    fi

    # 10. Verify backup is gone
    _list_out=$("$BIN" backup list --node "$TEST_NODE" --vmid "$TEST_ID" 2>&1) || true
    if echo "$_list_out" | grep -qF "$BACKUP_VOLID"; then
      RESULTS+=("FAIL  backup list no longer contains deleted volid")
      ((FAIL++))
    else
      RESULTS+=("PASS  backup list no longer contains deleted volid")
      ((PASS++))
    fi
  fi
fi

# ===========================================================================
# Section 4: Existing commands still work
# ===========================================================================

assert_output_contains \
  "vm list still works" \
  "VMID" \
  "$BIN" vm list

assert_output_contains \
  "node list still works" \
  "NODE" \
  "$BIN" node list

print_report
