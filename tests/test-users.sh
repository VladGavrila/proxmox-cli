#!/usr/bin/env bash
# Quick smoke tests for the pxve user command.
# Usage: ./tests/test-users.sh [binary]
#   binary defaults to ./dist/pxve-macos-arm64

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/helpers.sh"

resolve_bin "${1:-}"

echo "Running user CLI tests against $BIN ..."
echo ""

# ---------------------------------------------------------------------------
# Help / flag tests (no network required)
# ---------------------------------------------------------------------------

assert_output_contains \
  "user --help shows subcommands" \
  "Available Commands" \
  "$BIN" user --help

assert_output_contains \
  "user list --help" \
  "List all users" \
  "$BIN" user list --help

assert_output_contains \
  "user create --help shows --password flag" \
  "--password" \
  "$BIN" user create --help

assert_output_contains \
  "user create --help shows --email flag" \
  "--email" \
  "$BIN" user create --help

assert_output_contains \
  "user create --help shows --firstname flag" \
  "--firstname" \
  "$BIN" user create --help

assert_output_contains \
  "user create --help shows --lastname flag" \
  "--lastname" \
  "$BIN" user create --help

assert_output_contains \
  "user create --help shows --comment flag" \
  "--comment" \
  "$BIN" user create --help

assert_output_contains \
  "user create --help shows --groups flag" \
  "--groups" \
  "$BIN" user create --help

assert_output_contains \
  "user create --help shows --expire flag" \
  "--expire" \
  "$BIN" user create --help

assert_output_contains \
  "user delete --help shows userid arg" \
  "<userid>" \
  "$BIN" user delete --help

assert_output_contains \
  "user password --help shows --password flag" \
  "--password" \
  "$BIN" user password --help

assert_output_contains \
  "user token --help shows subcommands" \
  "Available Commands" \
  "$BIN" user token --help

assert_output_contains \
  "user token list --help shows userid arg" \
  "<userid>" \
  "$BIN" user token list --help

assert_output_contains \
  "user token create --help shows --no-privsep flag" \
  "--no-privsep" \
  "$BIN" user token create --help

assert_output_contains \
  "user token delete --help shows tokenid arg" \
  "<tokenid>" \
  "$BIN" user token delete --help

assert_output_contains \
  "user grant --help shows --role flag" \
  "--role" \
  "$BIN" user grant --help

assert_output_contains \
  "user grant --help shows --vmid flag" \
  "--vmid" \
  "$BIN" user grant --help

assert_output_contains \
  "user grant --help shows --path flag" \
  "--path" \
  "$BIN" user grant --help

assert_output_contains \
  "user grant --help shows --propagate flag" \
  "--propagate" \
  "$BIN" user grant --help

assert_output_contains \
  "user revoke --help shows --role flag" \
  "--role" \
  "$BIN" user revoke --help

assert_output_contains \
  "user revoke --help shows --vmid flag" \
  "--vmid" \
  "$BIN" user revoke --help

assert_output_contains \
  "user revoke --help shows --path flag" \
  "--path" \
  "$BIN" user revoke --help

# ---------------------------------------------------------------------------
# Argument validation (no network required)
# ---------------------------------------------------------------------------

assert_fail \
  "user create rejects missing args" \
  "$BIN" user create

assert_fail \
  "user delete rejects missing args" \
  "$BIN" user delete

assert_fail \
  "user password rejects missing args" \
  "$BIN" user password

assert_fail \
  "user token list rejects missing args" \
  "$BIN" user token list

assert_fail \
  "user token create rejects single arg" \
  "$BIN" user token create smoke@pve

assert_fail \
  "user token delete rejects single arg" \
  "$BIN" user token delete smoke@pve

assert_fail \
  "user grant rejects missing args" \
  "$BIN" user grant

assert_fail \
  "user revoke rejects missing args" \
  "$BIN" user revoke

assert_stderr_contains \
  "user create rejects invalid userid" \
  "invalid user ID" \
  "$BIN" user create 'baduser'

assert_stderr_contains \
  "user delete rejects invalid userid" \
  "invalid user ID" \
  "$BIN" user delete 'baduser'

assert_stderr_contains \
  "user password rejects invalid userid" \
  "invalid user ID" \
  "$BIN" user password 'baduser'

assert_stderr_contains \
  "user grant rejects invalid userid" \
  "invalid user ID" \
  "$BIN" user grant 'baduser'

assert_stderr_contains \
  "user revoke rejects invalid userid" \
  "invalid user ID" \
  "$BIN" user revoke 'baduser'

assert_stderr_contains \
  "user password fails without --password" \
  "required" \
  "$BIN" user password alice@pve

# ---------------------------------------------------------------------------
# CRUD lifecycle (requires a configured Proxmox instance)
# ---------------------------------------------------------------------------

# Create
assert_output_contains \
  "user create succeeds" \
  'User "smoke-test-usr@pve" created' \
  "$BIN" user create smoke-test-usr@pve --password initialpass --firstname Smoke --lastname Test --comment "Smoke test"

# List (table)
assert_output_contains \
  "user list shows created user" \
  "smoke-test-usr@pve" \
  "$BIN" user list

# List (json)
assert_output_contains \
  "user list -o json includes userid" \
  "smoke-test-usr@pve" \
  "$BIN" user list -o json

# Password (requires User.Modify on /access — may fail with restricted API tokens)
assert_output_contains \
  "user password succeeds" \
  "Password updated" \
  "$BIN" user password smoke-test-usr@pve --password newpass123

# Token create
assert_output_contains \
  "user token create succeeds" \
  "Token created" \
  "$BIN" user token create smoke-test-usr@pve smoke-tok

# Token list (table)
assert_output_contains \
  "user token list shows created token" \
  "smoke-tok" \
  "$BIN" user token list smoke-test-usr@pve

# Token list (json)
assert_output_contains \
  "user token list -o json includes token" \
  "smoke-tok" \
  "$BIN" user token list smoke-test-usr@pve -o json

# Token delete
assert_output_contains \
  "user token delete succeeds" \
  'Token "smoke-tok" deleted' \
  "$BIN" user token delete smoke-test-usr@pve smoke-tok

# Token gone after delete
assert_fail \
  "user token delete fails for already-deleted token" \
  "$BIN" user token delete smoke-test-usr@pve smoke-tok

# Grant
assert_output_contains \
  "user grant succeeds" \
  "Granted role" \
  "$BIN" user grant smoke-test-usr@pve --vmid 100 --role PVEVMUser

# Revoke
assert_output_contains \
  "user revoke succeeds" \
  "Revoked role" \
  "$BIN" user revoke smoke-test-usr@pve --vmid 100 --role PVEVMUser

# Delete
assert_output_contains \
  "user delete succeeds" \
  'User "smoke-test-usr@pve" deleted' \
  "$BIN" user delete smoke-test-usr@pve

# Delete non-existent user
assert_fail \
  "user delete fails for non-existent user" \
  "$BIN" user delete smoke-test-usr@pve

# ---------------------------------------------------------------------------
# Existing commands still work
# ---------------------------------------------------------------------------

assert_output_contains \
  "group list still works" \
  "GROUPID" \
  "$BIN" group list

assert_output_contains \
  "vm list still works" \
  "VMID" \
  "$BIN" vm list

# ---------------------------------------------------------------------------
# Report
# ---------------------------------------------------------------------------

print_report
