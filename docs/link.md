# Connect and repair bank connections

Run Hosted Link from a local terminal with a writable configuration:

```zsh
dinero link checking
dinero link --update checking
dinero --config /path/to/config.toml link savings --no-browser --timeout 15m
```

The configuration must include `[database]` and Plaid `client_id`, `secret`,
and `env` (`sandbox` or `production`). Default configuration resolution is
`--config`, then `DINERO_CONFIG_FILE`, then `~/.config/dinero/config.toml`
(or `$XDG_CONFIG_HOME/dinero/config.toml`). Link locks the configuration,
checks writable configuration and recovery storage, and rejects alias
collisions before contacting Plaid. An update alias must already exist.

The command creates a Hosted Link URL and opens it with the operating system's
browser opener. `--no-browser` prints the URL to stderr; an opener failure also
prints that fallback. Finish Link in your browser while the terminal polls.
The default deadline is ten minutes. Polling intervals are 2, 4, 8, then 10
seconds, capped by the remaining deadline. Ctrl-C cancels polling. Keep the
temporary Hosted Link URL private; it contains session state.

New connections exchange one public token and immediately save the access
token and Item ID before retrieving or naming accounts. Existing account names
are retained. New account names use `ALIAS - Provider Name`; duplicate names
get deterministic numeric suffixes such as `(2)`. You can adjust new names in
`[plaid.account_id_to_name]` afterward. Update mode supplies the existing access
token, omits products for ordinary credential repair, and performs no exchange.
A finished update session needs a success callback or HANDOFF event; an exit
is a failure. Empty, mismatched, or multiple-Item results are rejected.

Configuration writes preserve unrelated TOML values and use mode `0600`,
atomic replacement, and file/directory synchronization. A read-only secret
mount fails preflight. Link is a local CLI workflow; REST/MCP and the deployed
read-only server have no Link, token exchange, recovery upload, or filesystem
input endpoint.

## Recover a failed configuration write

If a new access token has been exchanged but a configuration write fails,
the command reports a protected recovery file path and exits unsuccessfully.
The record is written atomically with mode `0600` in a `0700` directory at
`$DINERO_STATE_DIR/link-recovery` (default: the Dinero configuration directory's
`link-recovery` subdirectory). Only that path is reported; tokens are never
printed. The record contains credentials, so keep it private and out of Git,
chat, logs, and deployment artifacts.

Once the configuration is writable again:

```zsh
dinero --config /path/to/config.toml link --resume /path/to/protected-recovery.json
```

Resume reads a regular local mode `0600` file belonging to the current user;
symlinks and malformed records are rejected. It performs no provider calls and
never exchanges again. It rejects a conflicting alias holding a different
token or Item ID and preserves existing account names. The recovery file is
deleted only after successful configuration persistence. A failed resume keeps
the record so the problem can be corrected and retried. When the first
credential write failed before naming began, resume saves credentials only;
run `dinero link --update ALIAS` afterward to populate account mappings.

If naming or account retrieval fails after credentials were saved, the new
connection remains in the configuration. An update can finish mappings. If both
configuration persistence and protected recovery fail, the command reports a
failure and makes no success or recovery-path claim. Cancellation after a
successful exchange still saves the newly obtained credentials, or writes
protected recovery, before returning the cancellation error.

Local configuration changes reach a deployed instance only through your
explicit external secret refresh and service restart/reload procedure. Link
does not modify Docker/Kubernetes secrets or redeploy the server. Run it against
a writable local copy, then update the deployed secret through your existing
deployment process.

## Optional manual Sandbox smoke test

Use a separate synthetic Sandbox configuration and isolated state directory.
Set `[plaid].env = "sandbox"` and your Sandbox application credentials locally;
do not paste credentials into shell arguments or output. Run `dinero --config
/path/to/sandbox-config.toml link sandbox_test --no-browser`, open the reported
URL, and complete the institution's Sandbox flow. Confirm the terminal reports
saved credentials, inspect aliases and mappings through `dinero config` with
the same `--config`, then run `link --update sandbox_test` against that file.
Keep configuration and state private and remove the disposable files when done.
Automated verification uses fake providers and loopback HTTP. This optional
manual check has not been run; production verification is a separate operation.
