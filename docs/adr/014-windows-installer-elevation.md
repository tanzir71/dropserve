# ADR-014: Windows installer elevation for reliable LAN access

Date: 2026-08-28 · Status: accepted · Supersedes: the no-admin packaging assumption in §12.1

## Context

Dropserve's primary Windows path must work from another device on the private network and uninstall without leaving a firewall rule. The handover also specifies an Inno Setup install under the current user's profile with no UAC prompt. Those requirements conflict on a default Windows installation: Microsoft documents Windows Firewall rule configuration as an administrator operation, and a non-administrative installer cannot reliably add or remove the inbound application rule.

The real `/CURRENTUSER` acceptance run confirmed the boundary. Dropserve installed and served locally, but a separate WSL2 guest timed out at the Windows gateway because no inbound rule existed. The same binary and route work when the administrative installer owns the exact private-profile rule.

## Options considered

1. Keep a non-administrative installer and rely on an eventual Windows Security prompt. This cannot make the silent install acceptance deterministic and risks leaving a path-based rule that the non-elevated uninstaller cannot remove.
2. Install without a rule and tell users to configure Windows Firewall themselves. This breaks the product's core phone/LAN path for the people least equipped to diagnose it.
3. Request elevation once during installation, add only the exact `Dropserve` private-profile rule for the installed desktop binary, and have the elevated uninstaller remove that rule plus autostart and trust state.

## Decision

Use option 3. The installer and uninstaller run in Inno Setup's administrative mode. The application and its current-user Scheduled Task still run as the signed-in user; Dropserve is not a Windows service and never runs its server as SYSTEM. The landing page and README name the one-time administrator prompt and its reason instead of claiming a no-admin install.

## Consequences

Windows installation has one UAC prompt. In return, the LAN path works without a firewall troubleshooting exercise, the rule is restricted to the installed executable and private profile, and silent uninstall can prove that the rule is gone. Portable builds remain available, but their users are responsible for allowing LAN access. Hosted installer tests must assert both the rule's exact executable and its removal.
