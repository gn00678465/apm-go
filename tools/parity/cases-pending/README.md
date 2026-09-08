# Pending parity cases (ticket 32)

Cases here are REAL, unwaivable Oracle gaps: the command's words and/or the
files it writes differ from the pinned Oracle in ways that are not rendering
residue. They are kept out of `cases/` so the fail-closed gate stays
meaningful (a waiver would be a lie; a red gate would block unrelated work),
and moved back one by one as `.scratch/parity-runner/issues/32-*.md` closes
them. Do not add a case here to dodge a failing comparison -- a new case
belongs in `cases/` unless it documents a gap already filed in ticket 32.
