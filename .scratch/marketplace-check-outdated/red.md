# RED reconstruction (tools/gate/red.sh -base main)

| Test file | Result at base | Note |
|---|---|---|
| cmd/apm-go/marketplace_check_config_test.go | failed (assertion) | 2 test(s) failed at base |
| cmd/apm-go/marketplace_check_label_test.go | failed (collection) | cmd\apm-go\marketplace_check_label_test.go:18:18: undefined: resolvingLabel |
| cmd/apm-go/marketplace_check_wording_test.go | failed (assertion) | 5 test(s) failed at base |
| cmd/apm-go/marketplace_outdated_wording_test.go | failed (assertion) | 3 test(s) failed at base |
| internal/marketplace/authoring/refcheck_outdated_sha_test.go | failed (collection) | internal\marketplace\authoring\refcheck_outdated_sha_test.go:22:49: unknown field Ref in struct literal of type "github.com/apm-go/apm/internal/semver".TagInfo |
| internal/marketplace/authoring/refcheck_sha_edges_test.go | failed (collection) | internal\marketplace\authoring\refcheck_sha_edges_test.go:23:12: undefined: manifestRepo |
| internal/marketplace/authoring/refcheck_sha_test.go | failed (collection) | internal\marketplace\authoring\refcheck_sha_test.go:45:17: undefined: CheckDeps |
| internal/marketplace/authoring/schema_validation_edges_test.go | failed (collection) | internal\marketplace\authoring\schema_validation_edges_test.go:8:12: undefined: asConfigValidationError |
| internal/marketplace/authoring/schema_validation_test.go | failed (collection) | internal\marketplace\authoring\schema_validation_test.go:88:8: undefined: IsConfigValidationError |

# Replay at each RED commit (assertion-level, git-reproducible)

| commit | package | assertion failures | compile errors |
|---|---|---|---|
| c702abb | ./internal/marketplace/authoring/ | assertion failures: 5 | compile errors: 0 |
| 452cfe7 | ./internal/marketplace/authoring/ | assertion failures: 18 | compile errors: 0 |
| 067d734 | ./internal/marketplace/authoring/ | assertion failures: 10 | compile errors: 0 |
| fdf5eca | ./internal/marketplace/authoring/ | assertion failures: 5 | compile errors: 0 |
