# Contributing to Dropserve

Every change must preserve these five product invariants:

1. A folder dropped into an Apps root is reachable over HTTP in under two seconds, with no manifest and no restart.
2. Dropserve never creates, modifies, or deletes any file inside an app folder.
3. Every URL surfaced in the interface resolves.
4. The server keeps serving healthy apps when any one app is broken, crashing, or missing.
5. Dropserve does not bind to a public interface, expose anything to the internet, or install anything into the operating-system trust store without an explicit, specific user action in that session.

Read [DROPSERVE-HANDOVER.md](DROPSERVE-HANDOVER.md) and [STATE.md](STATE.md) before changing the implementation. Work on the lowest-numbered unmet acceptance criterion, write the failing test first, and run `make check` before committing. Never weaken the gate to make a change pass.

Use conventional commit messages with the milestone in brackets, for example `feat(scanner): mount static folders [M1]`.

