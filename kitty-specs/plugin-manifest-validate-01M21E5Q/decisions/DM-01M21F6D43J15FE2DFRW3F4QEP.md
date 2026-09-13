# Decision Moment `01M21F6D43J15FE2DFRW3F4QEP`

- **Mission:** `plugin-manifest-validate-01M21E5Q`
- **Origin flow:** `plan`
- **Slot key:** `plan.architecture.rule-source`
- **Input key:** `rule_representation`
- **Status:** `resolved`
- **Created:** `2026-09-08T21:36:11.907180+00:00`
- **Resolved:** `2026-09-08T21:46:20.000984+00:00`
- **Opened by:** `claude`
- **Other answer:** `false`

## Question

How should the manifest field rules be represented: a hand-coded rule table in internal/pluginjson (stdlib only) with a test that keeps it in sync with the vendored official schema, or runtime JSON-schema validation using the jsonschema module already in go.mod?

## Options

- hand-coded rule table + schema sync test (recommended)
- runtime jsonschema validation against vendored schema
- Other

## Final answer

hand-coded rule table + schema sync test (recommended)

## Rationale

_(none)_

## Change log

- `2026-09-08T21:36:11.907180+00:00` — opened
- `2026-09-08T21:46:20.000984+00:00` — resolved (final_answer="hand-coded rule table + schema sync test (recommended)")
