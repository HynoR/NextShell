module nextshell.dev/v2

go 1.27.3

require (
	github.com/egoist/mygo v0.8.3
	github.com/google/uuid v1.6.0
	github.com/modelcontextprotocol/go-sdk v1.8.0
	github.com/pkg/sftp v1.13.12
	github.com/pressly/goose/v3 v3.30.0
	github.com/prometheus-community/pro-bing v0.9.1
	golang.org/x/crypto v0.58.0
	golang.org/x/net v0.62.0
	golang.org/x/text v0.44.0
	modernc.org/sqlite v1.62.0
)

require (
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/sethvargo/go-retry v0.5.0 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/image v0.46.0 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.52.1-0.20261106145633-d407941a339a // indirect
	modernc.org/libc v1.80.2 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

// Audited local fixes to the exact v0.8.3 source; see third_party/mygo/NEXTSHELL-PATCHES.md.
replace github.com/egoist/mygo => ./third_party/mygo
