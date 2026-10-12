# Pending parity cases (ticket 32)

This directory holds no case. Issue #50 closed ticket 32: its five cases are
in `cases/` and are compared by the gate.

A case belongs here only when it records a REAL, unwaivable Oracle gap that
is already filed in a ticket under `.scratch/parity-runner/issues/`: the
command's words or the files it writes differ from the Oracle in ways that
are not rendering residue. Such a case is kept out of `cases/` so the
fail-closed gate stays meaningful (a waiver would be a lie; a red gate would
block unrelated work), and it moves back when its ticket closes. Do not add
a case here to dodge a failing comparison -- a new case belongs in `cases/`.
