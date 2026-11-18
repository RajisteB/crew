---
name: go-backend-conventions
description: House rules for Go backend code (errors, context, logging, tests). Use whenever writing or reviewing Go in a backend service.
---
# Go backend conventions

Replace these with your team's actual rules; keep company specifics here, not in agents.

- Wrap errors with context: `fmt.Errorf("load payroll run %s: %w", id, err)`.
- Every exported function that does I/O takes `ctx context.Context` first.
- Table-driven tests; name cases after the behaviour, not the input.
- No new global state. Pass dependencies in.
- Run `go vet ./...` and the repo's linters before calling work done.
