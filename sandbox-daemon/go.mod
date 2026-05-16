module github.com/rommel-ade/rommel/sandbox-daemon

go 1.22

require (
	github.com/fsnotify/fsnotify v1.7.0
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.3
	github.com/rommel-ade/rommel/proto/clients/go v0.0.0-00010101000000-000000000000
)

require github.com/creack/pty v1.1.24 // indirect

// Until a top-level go.work lands, point at the in-tree generated client.
// The proto Go client is regenerated from proto/schemas/ via `make proto`
// (or proto/codegen/go.sh in CI).
replace github.com/rommel-ade/rommel/proto/clients/go => ../proto/clients/go
