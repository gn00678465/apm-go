# `doctor-network-auth-fail` — auth-failure stderr provenance

Ticket 08 eval attempt 2, finding 4a: an invalid credential against a
*public* repo (e.g. `github.com/git/git.git`, the URL doctor's own network
check actually calls) succeeds anonymously — GitHub never even inspects the
bad credential, since public repos don't require auth over HTTPS. That makes
"just try it against git/git.git" **not** a valid reproduction method for an
auth failure. This file is the checked-in, reproducible capture this
fixture's `FAKE_GIT_MODE=auth-fail` stderr text is verified against.

## Why this reproduces a real auth failure

GitHub cannot disclose, to an unauthenticated or misauthenticated caller,
whether a repo path is private or simply does not exist (that would leak
private-repo existence). So a request carrying invalid HTTP Basic credentials
against **any** path GitHub can't serve anonymously — private or
nonexistent — gets the same generic auth-challenge response `git` surfaces as
`fatal: Authentication failed for '...'`, never a "not found" message. The
credentials below are synthetic literals invented for this capture, never a
real account's — there is nothing to redact.

## Capture

```
$ git --version
git version 2.39.5

$ date -u +"%Y-%m-%dT%H:%M:%SZ"
2026-08-24T01:07:54Z

$ GIT_TERMINAL_PROMPT=0 git -c credential.helper= ls-remote \
    https://REDACTED-INVALID-USER:REDACTED-INVALID-PASSWORD@github.com/apm-go-parity-fixture-nonexistent-org/apm-go-parity-fixture-nonexistent-repo.git \
    HEAD
remote: Invalid username or token. Password authentication is not supported for Git operations.
fatal: Authentication failed for 'https://github.com/apm-go-parity-fixture-nonexistent-org/apm-go-parity-fixture-nonexistent-repo.git/'
$ echo $?
128
```

- `credential.helper=` and `GIT_TERMINAL_PROMPT=0` mirror `gitops.
  ApplySecureGitEnv`/`marketplace/git_stderr.py`'s own non-interactive
  contract (F07) — the capture never prompts or falls back to a system
  credential store.
- `apm-go-parity-fixture-nonexistent-org/apm-go-parity-fixture-nonexistent-repo`
  is namespaced to this project's own fixture naming specifically so it is
  never accidentally squatted by a real GitHub org/repo in the future,
  which would change this reproduction's behaviour (an existing PUBLIC repo
  at that path would go back to succeeding anonymously, same as
  `git/git.git`).

## What the fixture actually ships

`tools/parity/cases/doctor-network-auth-fail/path/git`'s `auth-fail` arm
prints the same two lines verified above, generalized to the URL doctor's
own network check actually invokes (`https://github.com/git/git.git`, per
`doctor.go`/`doctor.py:145-164`) rather than this capture's throwaway path —
only the repository URL inside the fatal line differs; the wording,
structure, and exit code (128) are the live capture verbatim. Re-run the
command above (with a fresh throwaway org/repo path, in case this one is
ever squatted) to re-verify before changing the fixture text.
