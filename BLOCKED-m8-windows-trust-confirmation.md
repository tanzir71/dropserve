# Blocked: M8 Windows trust confirmation

## Criterion

Verify once on real Windows that `dropserve trust --uninstall` removes the root added by `dropserve trust --install`.

## Attempts

1. Built the production CLI, generated a unique CA under an isolated `%LOCALAPPDATA%`, recorded thumbprint `F7FD64231ECEA916749608DA0AD105817E732209`, and ran `dropserve trust --install`. The process paused inside the Windows root-store call before adding the certificate; an independent `Cert:\CurrentUser\Root` query remained at zero. Cancelling the process left zero trusted matches.
2. Inspected the available Windows UI-automation route. Its mandatory safety policy explicitly forbids acting on Windows security prompts, so it cannot accept the root-trust warning on the user's behalf.
3. Checked Microsoft's documented non-UI alternatives. `CryptUIWizImport` with `CRYPTUI_WIZ_NO_UI` and `certutil -addstore` are different import paths from the handover-mandated `github.com/smallstep/truststore` production adapter; using one would not verify the command that ships and would silently change the specified design.

## Exact observed state

```text
production process: waiting during dropserve trust --install
thumbprint: F7FD64231ECEA916749608DA0AD105817E732209
Cert:\CurrentUser\Root count before cancellation: 0
Cert:\CurrentUser\Root count after cancellation: 0
generated root and private keys removed: yes
```

The isolated executable remains under the OS temp directory because this environment blocks deleting executables outside the workspace; it contains no certificate or key material and cannot mutate trust without a newly generated root.

## Best hypothesis

Windows requires an interactive security confirmation before accepting a new self-signed root into the current user's Trusted Root store. That prompt is the intended explicit-consent boundary, but it must be accepted by the user.

## Recovery

Run one visible isolated `dropserve trust --install`, accept the Windows warning, verify the unique thumbprint appears once, run `dropserve trust --uninstall`, and verify it returns to zero. Do not reuse the cancelled certificate; generate a fresh isolated CA and remove it immediately after the check.
