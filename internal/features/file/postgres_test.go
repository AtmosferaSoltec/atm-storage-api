package file

import (
	"context"
	"database/sql"
	"github.com/AtmosferaSoltec/atm-storage-api/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"os"
	"testing"
	"time"
)

func TestPostgresLifecycle(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not configured; use a dedicated disposable database")
	}
	ctx := context.Background()
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	goose.SetBaseFS(db.Migrations)
	if err = goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err = goose.UpContext(ctx, conn, "migrations"); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	r := NewRepository(pool)
	f := File{ID: uuid.NewString(), CompanyID: company, System: "olympus", Scope: "system", Purpose: "file", Filename: "test.txt", ContentType: "text/plain", Size: 5, CreatedAt: time.Now().UTC(), OwnerID: "owner", State: "pending", Provider: "r2", Bucket: "atm-dev", Key: uuid.NewString()}
	if err = r.Create(ctx, f); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM storage.files WHERE id=$1`, f.ID)
	stored, err := r.Get(ctx, f.ID)
	if err != nil || stored.State != "pending" {
		t.Fatal(stored, err)
	}
	if err = r.MarkReady(ctx, f.ID); err != nil {
		t.Fatal(err)
	}
	stored, err = r.Get(ctx, f.ID)
	if err != nil || stored.State != "ready" || stored.CompanyID != company {
		t.Fatal(stored, err)
	}
	if err = r.MarkDeleting(ctx, f.ID); err != nil {
		t.Fatal(err)
	}
	if err = r.MarkDeleting(ctx, f.ID); err != nil {
		t.Fatal("deletion retry", err)
	}
	if err = r.Delete(ctx, f.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Get(ctx, f.ID); err != ErrNotFound {
		t.Fatal(err)
	}
}
