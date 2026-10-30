# ATM Storage API

Shared file storage for ATM companies and applications. Go + Gin, PostgreSQL metadata, and a provider interface with an S3-compatible adapter for Cloudflare R2 (default) or AWS S3. Firebase is an extension point, not an implemented provider.

Company assets belong to the global company UUID from atm-auth-api. A company logo can therefore be reused across Olympus, La Vendimia and future systems. Application files remain scoped to their company, system and uploader.

This repository implements the storage service. The console storage module (usage overview, file list, upload, download and delete) is implemented. The company registry's `logo_file_id` field, existing-logo migration and consumer integrations are **not implemented yet**. See [architecture and integration decisions](docs/architecture.md).

## Structure

```text
cmd/api/                       CLI and dependency wiring
db/migrations/                 Embedded goose migrations
internal/config/               Environment loading and validation
internal/auth/                 Local verification of auth RS256 tokens
internal/features/file/        Domain, service, repository, SQL and Gin handlers
internal/provider/s3/          AWS SDK v2 adapter for R2 and S3
internal/platform/httpserver/  Shared HTTP response and middleware helpers
postman/specs/                 Authoritative OpenAPI contract
postman/mocks/                 Local R2 S3-subset simulation
postman/simulations/           Repeatable dependency tests and evidence
deploy/                        Container configuration templates
```

Gin, pgx, goose and godotenv follow the messaging service stack. The AWS SDK handles S3 signing/retries; golang-jwt validates RS256 tokens; google/uuid generates file identifiers. The domain has no AWS SDK or SQL dependency.

## Development

1. Provision a dedicated PostgreSQL database (`storage_dev`) and owner role (`storage_app`). Migrations create the `storage` schema. Never point tests at an application or production database.
2. Create a **private** R2 bucket named `atm-storage-dev`. Generate S3 Access Key ID/Secret Access Key restricted to that bucket. Cloudflare account API bearer tokens are not S3 credentials.
3. Copy `.env.example` to `.env`; fill database credentials, R2 settings and the **public** RSA key/issuer matching your auth development deployment. Never copy the auth private key. Use different signing keys and issuers in production.
4. Run:

```sh
go mod download
go run ./cmd/api migrate up
go run ./cmd/api serve
```

Environment variables already exported take precedence over `.env`. `migrate up|down|status` needs only `DATABASE_URL`. Migrations run explicitly, never automatically at service startup.

`APP_ENV=development` requires a bucket ending in `-dev`; `APP_ENV=production` requires `-prod`. There is no request-level bucket selection. All systems may share one bucket per environment, using internally generated company/system paths. Separate bucket-restricted credentials are the actual isolation boundary; names alone cannot enforce Cloudflare permissions.

For AWS S3 set `STORAGE_PROVIDER=s3`, the AWS region, credentials, and usually leave `STORAGE_ENDPOINT` empty. R2 uses `STORAGE_REGION=auto` and the account's HTTPS S3 endpoint. Changing providers/buckets requires migration of existing objects and their metadata; the service fails closed on location mismatches.

## API

All `/v1` routes require `Authorization: Bearer <atm-auth access token>`. The service verifies RS256, issuer, expiration, issued-at and subject locally. Token expiry governs revocation delay. Download URLs grant temporary access to their holder.

| Method | Route | Purpose |
| --- | --- | --- |
| GET | `/health` | Liveness |
| GET | `/ready` | PostgreSQL readiness (does not probe R2) |
| POST | `/v1/companies/{company_id}/files?scope=company&purpose=logo` | Superadmin uploads a shared logo |
| POST | `/v1/companies/{company_id}/files?scope=system&purpose=file&system=olympus` | Upload a private application file |
| GET | `/v1/files?company_id=&system=&scope=&limit=&offset=` | Superadmin lists ready files, newest first (`{items,total}`, page size up to 100) |
| GET | `/v1/files/summary` | Superadmin reads stored files and bytes per scope and system |
| GET | `/v1/files/{id}` | File metadata |
| GET | `/v1/files/{id}/download` | Signed download URL and expiry |
| POST | `/v1/files/preview` | Signed inline links for up to 100 images at once: `{ids:[…]}` → `{items:{<id>:{url,expires_at}}}`. Ids the caller cannot read or that are not images are left out. Links live `PREVIEW_URL_TTL` (1 h by default) |
| GET | `/v1/files/{id}/preview` | Signed URL that shows PNG/JPEG/GIF/WebP inline in the browser (415 `not_previewable` for other types) |
| DELETE | `/v1/files/{id}` | Delete object and metadata |

Upload body: `multipart/form-data` with exactly one `file` part. Default maximum: 25 MiB; logos: 2 MiB, valid PNG/JPEG, at most 16 megapixels. Generic files may contain arbitrary bytes and are downloaded as attachments; uploads are not malware-scanned. System uploads by ordinary users must match the token's `tenant` and `system`; their files are private to the uploader. Any member of a company can read its shared company assets; only superadmin can mutate them. A system's backend uses its service token from `POST /v1/token` in atm-auth-api to read company logos, and to **upload, read and delete the files of its own system** in the company it names (`scope=system&system=<its code>`); the business rules about who may do that live in that backend. It cannot touch another system's files, replace company assets, list or read usage. This service does not infer business permissions from `app_role`.

```sh
curl "$STORAGE_API_URL/v1/companies/$COMPANY_ID/files?scope=company&purpose=logo" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -F 'file=@logo.png'
```

A successful upload returns 201 and a stable `id` (plus metadata). Save that ID in the owning business record, never the signed download URL. An uploaded logo is not automatically assigned to a company. The future console integration must first save the new reference successfully, then remove the previous file if no longer referenced.

Responses use stable English error codes inside `{"error":{"code":"...","message":"..."}}`. Forbidden file reads/deletes return 404 to avoid exposing other tenants' files. Provider failures return 503. See [OpenAPI](postman/specs/storage.openapi.json) for details.

Uploads are not idempotent: an interrupted response may correspond to a completed upload. Do not blindly retry. Failed uploads retain pending metadata and possibly bytes for reconciliation. Failed deletions retain a deleting row and can be retried with the same ID; completed deletions return 404 on subsequent requests. Concurrent deletion may also return 404. Existing signed URLs stop working upon object deletion, not merely when metadata changes.

## Tests

```sh
go vet ./...
go test -race ./...
postman spec lint postman/specs/storage.openapi.json --issueType=syntax
```

The default Go run skips database/provider integration tests unless their explicit test variables are set. For a full local verification, use a disposable PostgreSQL container and Postman CLI 1.70+:

```sh
docker run -d --rm --name atm-storage-verification \
  -e POSTGRES_PASSWORD=storage_test_only -e POSTGRES_DB=storage_test \
  -p 127.0.0.1:55484:5432 postgres:17-alpine
export TEST_DATABASE_URL='postgres://postgres:storage_test_only@127.0.0.1:55484/storage_test?sslmode=disable'
DATABASE_URL="$TEST_DATABASE_URL" go run ./cmd/api migrate up

# Run in a separate terminal; keep it running while tests execute.
postman simulation run postman/simulations/storage-dev.sim.yaml \
  --port 4014 --no-history --output ndjson

# Back in the test terminal:
TEST_STORAGE_ENDPOINT=http://127.0.0.1:4014/r2 go test -race ./...

# Stop the simulation with Ctrl-C in its terminal; remove the disposable DB:
docker stop atm-storage-verification
```

The mock implements PUT/GET/DELETE and injected 503 responses from the documented S3 subset. It does not verify S3 signatures, credentials, CORS or real R2 behavior. Test control routes exist only in the local mock. No external workspace/mock is published. Postman owner-workspace lookup was unavailable while signed out; the local mock is based on the official provider documentation.

Verified on 2026-10-29: formatting, vet, race tests, PostgreSQL migrations/repository lifecycle, HTTP + JWT + PostgreSQL + AWS SDK pipeline against the Postman simulation, and OpenAPI syntax. The pipeline verified byte preservation, failed-upload pending metadata, denied cross-company/system access in domain tests, deletion outage visibility, and successful deletion retry. Live R2/AWS/Firebase and the container image have not been verified. Postman governance lint requires login and was not run successfully; syntax lint passed.

## Deploy

Pushing to the `production` branch runs `.github/workflows/deploy.yml`: gofmt, vet and race tests (against a throwaway PostgreSQL), image build, publish to GHCR and restart of the stack on the VPS. It follows the other APIs and needs the repository secrets `SSH_HOST`, `SSH_USER` and `SSH_KEY`.

| | |
|---|---|
| Domain | `storage.atmosferast.com` (host nginx, Cloudflare in front); site file in `deploy/host-nginx/` |
| Console | `console.atmosferast.com/storage-api/` forwards to the same container without the prefix (`deploy/host-nginx/console-storage-api.location`) |
| Container | `storage-api`, loopback port `3132`, network `atm-network` |
| Server folder | `/opt/storage-api` (`docker-compose.yml` is copied on every deploy) |
| Configuration | `/opt/storage-api/.env`, only on the server; template in `deploy/.env.production.example` |
| Database | `storage` owned by `storage_app` on the shared `atm-postgres` (no TLS: `sslmode=disable`) |

Each deploy runs `migrate up` in a one-shot container before the app starts; a failed migration aborts the deploy. The service never migrates by itself. The server `.env` needs the **production** R2 values (`atm-storage-prod` bucket and its own S3 key, never the development one) and the public key of the production auth-api. Set the reverse-proxy body limit to the file cap plus 64 KiB multipart overhead (26m) and configure request/concurrent-upload limits. The container's temporary filesystem is bounded to 512 MiB. Configure exact browser origins; no wildcard CORS.

Before production enablement, validate upload/download/delete against the dev R2 bucket and implement an operational reconciliation schedule for stale pending/deleting rows. There is no automatic cleanup worker yet. Inspect stale rows and object existence before cleanup; never delete an entire bucket shared by multiple systems. Bucket lifecycle rules must not expire active production objects.

References: [Cloudflare Go SDK example](https://developers.cloudflare.com/r2/examples/aws/aws-sdk-go/), [R2 S3 compatibility](https://developers.cloudflare.com/r2/api/s3/api/).
