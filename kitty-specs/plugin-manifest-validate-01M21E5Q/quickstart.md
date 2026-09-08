# Quickstart: Plugin manifest validate command

```sh
go build -o bin/apm-go ./cmd/apm-go

# 1. 合法 scaffold → 0 warnings, 0 errors, exit 0
bin/apm-go plugin init demo --yes --target claude
bin/apm-go plugin validate demo

# 2. 打錯欄位名 → warning + 建議, exit 0；--strict → exit 1
mkdir bad && printf '{"name":"x","descripton":"d"}' > bad/plugin.json
bin/apm-go plugin validate bad            # exit 0, 1 warning
bin/apm-go plugin validate bad --strict   # exit 1

# 3. 損壞的 manifest → 一行 Structure error, exit 1
mkdir broken && printf '{' > broken/plugin.json
bin/apm-go plugin validate broken

# 4. 測試與閘門
go test ./internal/pluginjson/ ./cmd/apm-go/
go test ./internal/pluginjson/ -run '^$' -fuzz FuzzValidateBytes -fuzztime 30s
sh tools/gate.sh            # evidence under .gate/plugin-manifest-validate/
```
