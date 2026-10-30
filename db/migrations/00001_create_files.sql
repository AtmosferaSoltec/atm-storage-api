-- +goose Up
CREATE SCHEMA IF NOT EXISTS storage;
CREATE TABLE storage.files (
 id uuid PRIMARY KEY,
 company_id uuid NOT NULL,
 system text NOT NULL DEFAULT '',
 scope text NOT NULL CHECK (scope IN ('company','system')),
 purpose text NOT NULL CHECK (purpose IN ('logo','file')),
 filename text NOT NULL,
 content_type text NOT NULL,
 size bigint NOT NULL CHECK (size > 0),
 created_at timestamptz NOT NULL DEFAULT now(),
 owner_id text NOT NULL,
 state text NOT NULL CHECK (state IN ('pending','ready','deleting')),
 provider text NOT NULL,
 bucket text NOT NULL,
 object_key text NOT NULL,
 UNIQUE (provider,bucket,object_key),
 CHECK ((scope='company' AND system='') OR (scope='system' AND system<>'')),
 CHECK (purpose<>'logo' OR scope='company')
);
CREATE INDEX files_company_system_idx ON storage.files(company_id,system);
CREATE INDEX files_incomplete_idx ON storage.files(created_at) WHERE state<>'ready';

-- +goose Down
DROP TABLE storage.files;
