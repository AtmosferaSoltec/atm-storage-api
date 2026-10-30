# Storage architecture

## Ownership

A company is the global tenant UUID issued by atm-auth-api. It is not a messaging tenant (which represents an application). Company assets have no system owner; system files have both a company UUID and a system code.

The company registry remains in auth. Storage owns bytes and file metadata, not company names or subscriptions. The planned console workflow uploads a company logo, then saves its stable file ID on the company record. Systems read that reference and resolve a short-lived download URL. Never persist signed URLs as company data. Updating the company DTO/database and migrating Olympus/La Vendimia logos are separate integration work, not implemented by this service scaffold.

## Initial contract

- Go + Gin; feature packages; handler -> service -> repository interfaces.
- PostgreSQL metadata with goose migrations, separate database/role per environment.
- Provider interface: Put, Delete, DownloadURL. R2 and S3 share an AWS SDK v2 adapter. Firebase will need a separate implementation; switching provider does not migrate existing bytes.
- One deployment uses one configured provider/bucket. APP_ENV must match the bucket suffix (-dev or -prod). Requests cannot choose environment, bucket or object key.
- Separate R2 keys restricted to each bucket, databases and auth signing keys/issuers for development and production. Bucket suffix validation catches naming mistakes, but bucket-scoped credentials enforce isolation.
- Private bucket only. Every download requires authorization before issuing a five-minute signed URL. Signed URLs are bearer capabilities until expiry. No public custom-domain URLs in this initial version.
- RS256 auth tokens verified locally using the configured public key and issuer. No auth request per upload. Key rotation requires updating configuration/restarting.
- Superadmin may upload/manage any company asset or system file. Ordinary user tokens may upload system files only within their token's company/system; access to those private files is limited to the uploader. All users in a company may read its shared company assets. Service tokens (role `service`, issued to a system's backend) read company logos and store, read and delete the files of their own system, in the company they name: the token names the system, not a company, and the backend vouches for the company. They never replace company assets, reach another system's files, list files or read usage. Files a backend stores are owned by `system:<code>`, so every user of the company is served through that backend rather than by this API. This intentionally does not infer application roles from app_role; broader sharing requires an explicit future policy.
- Company ID existence for superadmin uploads is trusted to the caller (console must select an existing company); there is no distributed foreign key.
- Upload via multipart form, exactly one `file` part, bounded disk spool (default 25 MiB), default logo cap 2 MiB. Logo bytes must decode as PNG or JPEG with at most 16 megapixels. Generic files are stored as detected media type, downloaded as attachments. No malware scanning or executable-content approval is implied.
- Metadata is inserted as pending before object upload, exposed only after ready. Failures leave pending rows for operator reconciliation; pending IDs are never returned as successful uploads. An upload is not idempotent: clients must not blindly retry after ambiguous timeouts.
- Deletion marks deleting before touching the provider; download access stops immediately. A repeat DELETE resumes cleanup. Metadata is removed only after provider deletion succeeds. Existing signed URLs may remain usable until object deletion or expiration.
- Object keys contain environment/company/scope/system/random ID, never user filenames. Provider, bucket and key are retained in internal metadata so configuration changes fail closed for old files rather than deleting objects from another destination.

## Operations

GET /health is liveness. GET /ready checks PostgreSQL only; provider credentials are validated by the first provider operation. Max request duration is 90 seconds, graceful shutdown 15 seconds. TLS terminates at the reverse proxy. Set its body limit to the upload cap plus multipart overhead. Limit concurrent uploads at the gateway to bound temporary disk usage. No automatic production deployment or bucket creation.

Before release, run a live R2 upload/download/delete using isolated development credentials, configure quotas/rate limits at the gateway, and establish a reconciliation job for stale pending/deleting rows. Never delete all bucket contents to clean a development environment shared by systems.
