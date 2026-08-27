# Blocked: M0 hosted CI

**Criterion:** CI is green on both `ubuntu-latest` and `windows-latest`.

## Blocking condition

The authoritative repository `github.com/tanzir71/dropserve` does not exist. Running the required hosted jobs therefore requires creating and publishing a new GitHub repository under the user's account. That external publication has not been authorized, so no remote was created and no code was pushed.

## Attempts

1. Created and locally verified the complete CI workflow, then queried the required repository. GitHub returned:

   ```text
   GraphQL: Could not resolve to a Repository with the name 'tanzir71/dropserve'. (repository)
   ```

2. Updated every workflow action to its current official release and ran `actionlint v1.7.12`; it reported no findings.
3. Ran the hosted jobs' local equivalents: `make check` passed, `golangci-lint v2.13.1` reported zero issues, and `gitleaks v8.30.1` found no leaks. A final GitHub query produced the same missing-repository error.

## Best hypothesis

This is an external-state and authorization blocker, not a source or workflow failure. The handover explicitly forbids advancing beyond M0 until hosted CI is green.

## How to unblock

Either authorize creation of a public `tanzir71/dropserve` repository, or create that repository and provide it as the push target. Then push `main`, wait for all Ubuntu and Windows jobs, fix any runner-specific failures, and record the successful run in `STATE.md` before tagging `m0-complete`.

