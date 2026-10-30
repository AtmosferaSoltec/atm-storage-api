<API_TASKS>
Before responding to any non-trivial API engineering task — designing, implementing, mocking, testing, monitoring, documenting, or deploying an API or Postman Flow — read `postman/skills/api-engineer/SKILL.md` and follow it. It is the default entry point for API work in this repository. Pure questions and trivial one-line edits do not need it.
</API_TASKS>

# ATM-Storage-API

Use English for code, comments, documentation, routes and error messages. Talk to the user in Spanish.

Go + Gin, PostgreSQL with pgx and goose. Business modules live in `internal/features/`. Keep handler -> service -> repository; provider SDK imports belong only in provider adapters. Entry points only wire dependencies. Define small interfaces where consumed. Do not couple domain logic to R2 or SQL.

Never accept bucket, provider credentials, environment or object keys from callers. Company IDs are global auth tenant UUIDs. Verify authorization for each file operation. Never log JWTs or signed URLs. Separate development and production credentials and databases.

Run `gofmt`, `go vet ./...`, and `go test -race ./...`. Integration tests use only explicit test endpoints/databases. Keep the OpenAPI contract and README updated. Do not modify other services as part of an unrequested refactor.
