package persistence

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/direnv/direnv/v2/sri"
	"github.com/georgysavva/scany/pgxscan"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v4"
	"github.com/pkg/errors"

	"github.com/input-output-hk/cicero/src/config"
	"github.com/input-output-hk/cicero/src/domain"
	"github.com/input-output-hk/cicero/src/domain/repository"
)

type factRepository struct {
	DB config.PgxIface
}

func NewFactRepository(db config.PgxIface) repository.FactRepository {
	return &factRepository{db}
}

func (a *factRepository) WithQuerier(querier config.PgxIface) repository.FactRepository {
	return &factRepository{querier}
}

func (a *factRepository) GetById(id uuid.UUID) (fact domain.Fact, err error) {
	err = pgxscan.Get(
		context.Background(), a.DB, &fact,
		`SELECT id, run_id, value, created_at, binary_hash FROM fact WHERE id = $1`,
		id,
	)
	return
}

func (a *factRepository) GetByRunId(id uuid.UUID) (facts []*domain.Fact, err error) {
	err = pgxscan.Select(
		context.Background(), a.DB, &facts,
		`SELECT id, run_id, value, created_at, binary_hash
		FROM fact WHERE run_id = $1
		ORDER BY created_at DESC`,
		id,
	)
	return
}

func (a *factRepository) GetBinaryById(tx pgx.Tx, id uuid.UUID) (binary io.ReadSeekCloser, err error) {
	var oid uint32
	err = pgxscan.Get(
		context.Background(), tx, &oid,
		`SELECT "binary" FROM fact WHERE id = $1`,
		id,
	)
	if err != nil {
		return
	}

	los := tx.LargeObjects()
	binary, err = los.Open(context.Background(), oid, pgx.LargeObjectModeRead)
	err = errors.WithMessagef(err, "Failed to open large object with OID %d", oid)

	return
}

func (a *factRepository) GetLatestByFields(fields [][]string) (fact domain.Fact, err error) {
	err = pgxscan.Get(
		context.Background(), a.DB, &fact,
		`SELECT id, run_id, value, created_at, binary_hash FROM fact `+sqlWhereHasPaths(fields)+` ORDER BY created_at DESC FETCH FIRST ROW ONLY`,
		pathsToQueryArgs(fields)...,
	)
	return
}

func (a *factRepository) GetByFields(fields [][]string) (facts []*domain.Fact, err error) {
	err = pgxscan.Select(
		context.Background(), a.DB, &facts,
		`SELECT id, run_id, value, created_at, binary_hash FROM fact `+sqlWhereHasPaths(fields),
		pathsToQueryArgs(fields)...,
	)
	return
}

func sqlWhereHasPaths(paths [][]string) (where string) {
	if len(paths) == 0 {
		return
	}

	where += ` WHERE `
	n := 1
	for i, path := range paths {
		if i > 0 {
			where += ` AND `
		}
		where += ` jsonb_extract_path(value `
		for range path {
			where += ` , $` + strconv.Itoa(n)
			n += 1
		}
		where += ` ) IS NOT NULL `
	}

	return
}

func pathsToQueryArgs(paths [][]string) (args []interface{}) {
	for _, path := range paths {
		for _, field := range path {
			args = append(args, field)
		}
	}
	return
}

func (a *factRepository) Save(fact *domain.Fact, binary io.Reader) error {
	ctx := context.Background()
	return a.DB.BeginFunc(ctx, func(tx pgx.Tx) error {
		var binaryOid *uint32
		if binary != nil {
			los := tx.LargeObjects()
			if oid, err := los.Create(ctx, 0); err != nil {
				return errors.WithMessage(err, "Failed to create large object")
			} else if lo, err := los.Open(ctx, oid, pgx.LargeObjectModeWrite); err != nil {
				return errors.WithMessagef(err, "Failed to open large object with OID %d", oid)
			} else {
				hash := sri.NewWriter(lo, sri.SHA256)
				switch written, err := io.Copy(hash, binary); {
				case err != nil:
					return errors.WithMessagef(err, "Failed to write to large object with OID %d", oid)
				case written == 0:
					// Nothing was written because the read stream was empty.
					// We treat that case as if we had `binary == nil`.
					if err := los.Unlink(ctx, oid); err != nil {
						return errors.WithMessagef(err, "Failed to unlink large object with OID %d", oid)
					}
				default:
					sum := hash.Sum()
					if fact.BinaryHash != nil && *fact.BinaryHash != sum {
						return fmt.Errorf("Binary has hash %q instead of expected %q", sum, *fact.BinaryHash)
					}
					fact.BinaryHash = &sum
					binaryOid = &oid
				}
			}
		}

		return pgxscan.Get(
			ctx, tx, fact,
			`INSERT INTO fact (run_id, value, binary_hash, "binary") VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
			fact.RunId, fact.Value, fact.BinaryHash, binaryOid,
		)
	})
}
