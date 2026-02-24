#!/usr/bin/env bash
# Quick smoke tests for the pxve group command.
# Usage: ./tests/test-groups.sh [binary]
#   binary defaults to ./dist/pxve-macos-arm64

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/helpers.sh"

resolve_bin "${1:-}"

echo "Running group CLI tests against $BIN ..."
echo ""

# ---------------------------------------------------------------------------
# Help / alias tests (no network required)
# ---------------------------------------------------------------------------

assert_output_contains \
  "group --help shows subcommands" \
  "Available Commands" \
  "$BIN" group --help

assert_output_contains \
  "grp alias works" \
  "Available Commands" \
  "$BIN" grp --help

assert_output_contains \
  "group list --help" \
  "List all groups" \
  "$BIN" group list --help

assert_output_contains \
  "group create --help shows --comment flag" \
  "--comment" \
  "$BIN" group create --help

assert_output_contains \
  "group delete --help shows --force flag" \
  "--force" \
  "$BIN" group delete --help

assert_output_contains \
  "group show --help" \
  "Show group details" \
  "$BIN" group show --help

# ---------------------------------------------------------------------------
# Argument validation (no network required)
# ---------------------------------------------------------------------------

assert_fail \
  "group create rejects missing args" \
  "$BIN" group create

assert_fail \
  "group show rejects missing args" \
  "$BIN" group show

assert_fail \
  "group delete rejects missing args" \
  "$BIN" group delete

assert_stderr_contains \
  "group create rejects invalid groupid" \
  "invalid group ID" \
  "$BIN" group create 'bad group!!'

assert_stderr_contains \
  "group delete rejects invalid groupid" \
  "invalid group ID" \
  "$BIN" group delete 'inv@lid'

# ---------------------------------------------------------------------------
# CRUD lifecycle (requires a configured Proxmox instance)
# ---------------------------------------------------------------------------

# Create
assert_output_contains \
  "group create succeeds" \
  'Group "smoke-test-grp" created' \
  "$BIN" group create smoke-test-grp --comment "Smoke test"

# List (table)
assert_output_contains \
  "group list shows created group" \
  "smoke-test-grp" \
  "$BIN" group list

# List (json)
assert_output_contains \
  "group list -o json includes groupid" \
  "smoke-test-grp" \
  "$BIN" group list -o json

# Show
assert_output_contains \
  "group show displays group" \
  "smoke-test-grp" \
  "$BIN" group show smoke-test-grp

assert_output_contains \
  "group show displays comment" \
  "Smoke test" \
  "$BIN" group show smoke-test-grp

assert_output_contains \
  "group show -o json works" \
  "smoke-test-grp" \
  "$BIN" group show smoke-test-grp -o json

# Delete — cancel with 'n'
assert_output_contains \
  "group delete (cancel with n)" \
  "Cancelled" \
  bash -c "echo n | $BIN group delete smoke-test-grp"

# Verify still exists after cancel
assert_output_contains \
  "group still exists after cancelled delete" \
  "smoke-test-grp" \
  "$BIN" group list

# Delete — confirm with 'y'
assert_output_contains \
  "group delete (confirm with y)" \
  'Group "smoke-test-grp" deleted' \
  bash -c "echo y | $BIN group delete smoke-test-grp"

# Create + force delete
assert \
  "group create for --force test" \
  "$BIN" group create smoke-test-force

assert_output_contains \
  "group delete --force skips prompt" \
  'Group "smoke-test-force" deleted' \
  "$BIN" group delete smoke-test-force --force

# Show non-existent group
assert_fail \
  "group show non-existent group fails" \
  "$BIN" group show no-such-group-xyz

assert_fail \
  "group delete non-existent group fails" \
  "$BIN" group delete no-such-group-xyz --force

# ---------------------------------------------------------------------------
# Membership commands — help / validation (no network required)
# ---------------------------------------------------------------------------

assert_output_contains \
  "group add-member --help shows usage" \
  "Add a user to a group" \
  "$BIN" group add-member --help

assert_output_contains \
  "group remove-member --help shows usage" \
  "Remove a user from a group" \
  "$BIN" group remove-member --help

assert_fail \
  "group add-member rejects missing args" \
  "$BIN" group add-member

assert_fail \
  "group add-member rejects single arg" \
  "$BIN" group add-member mygroup

assert_fail \
  "group remove-member rejects missing args" \
  "$BIN" group remove-member

assert_fail \
  "group remove-member rejects single arg" \
  "$BIN" group remove-member mygroup

assert_stderr_contains \
  "group add-member rejects invalid groupid" \
  "invalid group ID" \
  "$BIN" group add-member 'bad group!!' alice@pve

assert_stderr_contains \
  "group add-member rejects invalid userid" \
  "invalid user ID" \
  "$BIN" group add-member mygroup 'notauser'

assert_stderr_contains \
  "group remove-member rejects invalid groupid" \
  "invalid group ID" \
  "$BIN" group remove-member 'inv@lid' alice@pve

assert_stderr_contains \
  "group remove-member rejects invalid userid" \
  "invalid user ID" \
  "$BIN" group remove-member mygroup 'notauser'

# ---------------------------------------------------------------------------
# Membership CRUD lifecycle (requires a configured Proxmox instance)
# ---------------------------------------------------------------------------

# Setup: create a group and a user for membership testing.
assert \
  "membership: create test group" \
  "$BIN" group create smoke-member-grp --comment "Membership test"

assert \
  "membership: create test user" \
  "$BIN" user create smoke-member@pve

# Add member
assert_output_contains \
  "group add-member succeeds" \
  'User "smoke-member@pve" added to group "smoke-member-grp"' \
  "$BIN" group add-member smoke-member-grp smoke-member@pve

# Verify member appears in group show
assert_output_contains \
  "group show lists added member" \
  "smoke-member@pve" \
  "$BIN" group show smoke-member-grp

# Adding same member again should fail (already a member)
assert_fail \
  "group add-member rejects duplicate" \
  "$BIN" group add-member smoke-member-grp smoke-member@pve

# Remove member
assert_output_contains \
  "group remove-member succeeds" \
  'User "smoke-member@pve" removed from group "smoke-member-grp"' \
  "$BIN" group remove-member smoke-member-grp smoke-member@pve

# Verify member no longer listed
assert_output_contains \
  "group show reports no members after removal" \
  "No members" \
  "$BIN" group show smoke-member-grp

# Removing non-member should fail
assert_fail \
  "group remove-member fails for non-member" \
  "$BIN" group remove-member smoke-member-grp smoke-member@pve

# Cleanup
assert \
  "membership: delete test user" \
  "$BIN" user delete smoke-member@pve

assert \
  "membership: delete test group" \
  "$BIN" group delete smoke-member-grp --force

# ---------------------------------------------------------------------------
# Existing commands still work
# ---------------------------------------------------------------------------

assert_output_contains \
  "user list still works" \
  "USERID" \
  "$BIN" user list

assert_output_contains \
  "vm list still works" \
  "VMID" \
  "$BIN" vm list

# ---------------------------------------------------------------------------
# Report
# ---------------------------------------------------------------------------

print_report
