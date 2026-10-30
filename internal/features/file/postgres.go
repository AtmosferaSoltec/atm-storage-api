package file

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }
func (r *Postgres) Create(ctx context.Context, f File) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO storage.files(id,company_id,system,scope,purpose,filename,content_type,size,created_at,owner_id,state,provider,bucket,object_key) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, f.ID, f.CompanyID, f.System, f.Scope, f.Purpose, f.Filename, f.ContentType, f.Size, f.CreatedAt, f.OwnerID, f.State, f.Provider, f.Bucket, f.Key)
	return err
}
func (r *Postgres) Get(ctx context.Context, id string) (File, error) {
	var f File
	err := r.pool.QueryRow(ctx, `SELECT id,company_id,system,scope,purpose,filename,content_type,size,created_at,owner_id,state,provider,bucket,object_key FROM storage.files WHERE id=$1`, id).Scan(&f.ID, &f.CompanyID, &f.System, &f.Scope, &f.Purpose, &f.Filename, &f.ContentType, &f.Size, &f.CreatedAt, &f.OwnerID, &f.State, &f.Provider, &f.Bucket, &f.Key)
	if errors.Is(err, pgx.ErrNoRows) {
		return File{}, ErrNotFound
	}
	return f, err
}
func (r *Postgres) MarkReady(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE storage.files SET state='ready' WHERE id=$1 AND state='pending'`, id)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return err
}
func (r *Postgres) MarkDeleting(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE storage.files SET state='deleting' WHERE id=$1 AND state IN ('ready','deleting')`, id)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return err
}
func (r *Postgres) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM storage.files WHERE id=$1 AND state='deleting'`, id)
	return err
}
func (r *Postgres) List(ctx context.Context, f ListFilter) ([]File, int, error) {
	where, args := "state='ready'", []any{}
	for column, value := range map[string]string{"company_id": f.CompanyID, "system": f.System, "scope": f.Scope} {
		if value != "" {
			args = append(args, value)
			where += fmt.Sprintf(" AND %s=$%d", column, len(args))
		}
	}
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM storage.files WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.Limit, f.Offset)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT id,company_id,system,scope,purpose,filename,content_type,size,created_at FROM storage.files WHERE %s ORDER BY created_at DESC, id LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []File{}
	for rows.Next() {
		var item File
		if err = rows.Scan(&item.ID, &item.CompanyID, &item.System, &item.Scope, &item.Purpose, &item.Filename, &item.ContentType, &item.Size, &item.CreatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}
func (r *Postgres) Usage(ctx context.Context) ([]Usage, error) {
	rows, err := r.pool.Query(ctx, `SELECT scope,system,count(*),coalesce(sum(size),0) FROM storage.files WHERE state='ready' GROUP BY scope,system ORDER BY sum(size) DESC, system`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	usage := []Usage{}
	for rows.Next() {
		var u Usage
		if err = rows.Scan(&u.Scope, &u.System, &u.FilesCount, &u.TotalBytes); err != nil {
			return nil, err
		}
		usage = append(usage, u)
	}
	return usage, rows.Err()
}
