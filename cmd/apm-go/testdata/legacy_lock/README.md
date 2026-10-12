`apm.lock.yaml` is what apm-go at commit `94f1aab` (before issue #50) wrote
for `apm-go install ./dep` in a project with one local package and one
instruction of its own. It is not hand-written; `issue50_test.go` builds the
project around it. The key `_local/dep-493bc424` ends with a hash of the
absolute source path, so the test replaces it with the key for its own
temporary directory.
