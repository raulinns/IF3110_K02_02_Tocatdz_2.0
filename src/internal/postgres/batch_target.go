package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/targetbatch"
)

// Role runtime hanya bisa menulis checkpoint lewat fungsi dengan bukti HMAC, sehingga SQL migration
// tidak bisa memalsukannya.
const writeCheckpointSignature = "write_checkpoint(uuid,text,bigint,bigint,bigint,bigint,bigint,bigint[],boolean,bytea)"
const lookupCheckpointSignature = "lookup_checkpoints(uuid,text,bigint,bigint,integer,boolean)"

func batchSupportStatements(owner, runtime string) []string {
	ownerID, runtimeID := pgx.Identifier{owner}.Sanitize(), pgx.Identifier{runtime}.Sanitize()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS tocatdz_internal.batch_checkpoints (
			execution_id uuid NOT NULL, direction text NOT NULL CHECK (direction IN ('up','down')),
			batch_no bigint NOT NULL CHECK (batch_no > 0), cursor_from bigint, cursor_to bigint NOT NULL,
			scope_max bigint NOT NULL, rows bigint NOT NULL CHECK (rows >= 0), keys bigint[] NOT NULL,
			final boolean NOT NULL DEFAULT false, recorded_at timestamptz NOT NULL DEFAULT now(),
			PRIMARY KEY (execution_id, direction, batch_no))`,
		"ALTER TABLE tocatdz_internal.batch_checkpoints OWNER TO " + ownerID,
		"REVOKE ALL ON tocatdz_internal.batch_checkpoints FROM PUBLIC," + runtimeID,
		`CREATE OR REPLACE FUNCTION tocatdz_internal.write_checkpoint(e uuid, dir text, b bigint, cf bigint, ct bigint, sm bigint,
			r bigint, k bigint[], fin boolean, p bytea) RETURNS void
		LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,tocatdz_internal,pg_temp AS $body$
		DECLARE secret bytea; expected bytea;
		BEGIN
			SELECT key INTO STRICT secret FROM tocatdz_internal.receipt_keys WHERE singleton;
			expected := tocatdz_internal.hmac(convert_to(concat_ws('|','tocatdz/checkpoint/v1',e::text,dir,b::text,
				coalesce(cf::text,''),ct::text,sm::text,r::text,array_to_string(k,','),fin::text),'UTF8'),secret,'sha256');
			IF e IS NULL OR dir NOT IN ('up','down') OR b IS NULL OR b <= 0 OR ct IS NULL OR sm IS NULL OR r IS NULL OR r < 0
				OR k IS NULL OR fin IS NULL OR p IS NULL OR p <> expected THEN
				RAISE EXCEPTION 'invalid checkpoint proof' USING ERRCODE='42501';
			END IF;
			INSERT INTO tocatdz_internal.batch_checkpoints(execution_id,direction,batch_no,cursor_from,cursor_to,scope_max,rows,keys,final)
			VALUES(e,dir,b,cf,ct,sm,r,k,fin);
		END $body$`,
		`CREATE OR REPLACE FUNCTION tocatdz_internal.lookup_checkpoints(e uuid, dir text, after_b bigint, before_b bigint, lim integer,
			descending boolean)
		RETURNS TABLE(batch_no bigint, cursor_from bigint, cursor_to bigint, scope_max bigint, rows bigint, keys bigint[], final boolean,
			recorded_at timestamptz)
		LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,tocatdz_internal,pg_temp AS $body$
			SELECT c.batch_no, c.cursor_from, c.cursor_to, c.scope_max, c.rows, c.keys, c.final, c.recorded_at
			FROM tocatdz_internal.batch_checkpoints c
			WHERE c.execution_id = e AND c.direction = dir AND (after_b IS NULL OR c.batch_no > after_b)
				AND (before_b IS NULL OR c.batch_no < before_b)
			ORDER BY CASE WHEN descending THEN -c.batch_no ELSE c.batch_no END
			LIMIT lim
		$body$`,
	}
	for _, signature := range []string{writeCheckpointSignature, lookupCheckpointSignature} {
		statements = append(statements,
			"ALTER FUNCTION tocatdz_internal."+signature+" OWNER TO "+ownerID,
			"REVOKE ALL ON FUNCTION tocatdz_internal."+signature+" FROM PUBLIC",
			"GRANT EXECUTE ON FUNCTION tocatdz_internal."+signature+" TO "+runtimeID)
	}
	return statements
}

func ensureBatchSupport(ctx context.Context, tx pgx.Tx, owner, runtime string) error {
	for _, statement := range batchSupportStatements(owner, runtime) {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return security.ErrUnavailable
		}
	}
	return nil
}

// UpgradeManagedTargetBatch menambah dukungan batch pada target lama. Butuh DSN admin target.
func UpgradeManagedTargetBatch(ctx context.Context, adminDSN security.Secret) error {
	conn, err := pgx.Connect(ctx, adminDSN.Reveal())
	if err != nil {
		return security.ErrTargetNotReady
	}
	defer conn.Close(context.WithoutCancel(ctx))
	var oid uint32
	if err := conn.QueryRow(ctx, "SELECT oid FROM pg_catalog.pg_database WHERE datname=current_database()").Scan(&oid); err != nil {
		return security.ErrTargetNotReady
	}
	owner := "tocatdz_receipts_" + strconv.FormatUint(uint64(oid), 10)
	runtime := "tocatdz_exec_" + strconv.FormatUint(uint64(oid), 10)
	tx, err := conn.Begin(ctx)
	if err != nil {
		return security.ErrUnavailable
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err := tx.Exec(ctx, "SELECT pg_catalog.pg_advisory_xact_lock($1)", int64(targetLockKey)); err != nil {
		return security.ErrUnavailable
	}
	var ready bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass('tocatdz_internal.receipt_keys') IS NOT NULL
		AND EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1) AND EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $2)`, owner, runtime).Scan(&ready); err != nil || !ready {
		return security.ErrTargetNotReady
	}
	if err := ensureBatchSupport(ctx, tx, owner, runtime); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return security.ErrUnavailable
	}
	return nil
}

func (b *SecretBox) checkpointProof(checkpoint targetbatch.Checkpoint, scope security.ID) []byte {
	keys := make([]string, len(checkpoint.Keys))
	for i, key := range checkpoint.Keys {
		keys[i] = strconv.FormatInt(key, 10)
	}
	from := ""
	if checkpoint.From != nil {
		from = strconv.FormatInt(*checkpoint.From, 10)
	}
	mac := hmac.New(sha256.New, b.receiptSigningKey(scope))
	_, _ = mac.Write([]byte(strings.Join([]string{"tocatdz/checkpoint/v1", string(checkpoint.ExecutionID), string(checkpoint.Direction),
		strconv.FormatInt(checkpoint.Batch, 10), from, strconv.FormatInt(checkpoint.To, 10), strconv.FormatInt(checkpoint.ScopeMax, 10),
		strconv.FormatInt(checkpoint.Rows, 10), strings.Join(keys, ","), strconv.FormatBool(checkpoint.Final)}, "|")))
	return mac.Sum(nil)
}

var _ targetbatch.Target = (*lockedTarget)(nil)

func (l *lockedTarget) BatchSupported(ctx context.Context) error {
	var supported bool
	err := l.conn.QueryRow(ctx, "SELECT to_regprocedure($1) IS NOT NULL AND to_regprocedure($2) IS NOT NULL",
		"tocatdz_internal."+writeCheckpointSignature, "tocatdz_internal."+lookupCheckpointSignature).Scan(&supported)
	if err != nil {
		return targetError(ctx, err)
	}
	if !supported {
		return security.ErrTargetNotReady
	}
	return nil
}

func (l *lockedTarget) KeyColumn(ctx context.Context, schema, table, column string) (targetbatch.KeyInfo, error) {
	var info targetbatch.KeyInfo
	err := l.conn.QueryRow(ctx, `SELECT true, a.atttypid IN ('int2'::regtype, 'int4'::regtype, 'int8'::regtype), a.attnotnull,
			EXISTS (SELECT 1 FROM pg_catalog.pg_index i WHERE i.indrelid = c.oid AND i.indisunique AND i.indnkeyatts = 1
				AND i.indkey[0] = a.attnum AND i.indpred IS NULL AND i.indexprs IS NULL)
		FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid
		WHERE n.nspname = $1 AND c.relname = $2 AND a.attname = $3 AND a.attnum > 0 AND NOT a.attisdropped AND c.relkind IN ('r', 'p')`,
		schema, table, column).Scan(&info.Exists, &info.Integer, &info.NotNull, &info.Unique)
	if errors.Is(err, pgx.ErrNoRows) {
		return targetbatch.KeyInfo{}, nil
	}
	if err != nil {
		return targetbatch.KeyInfo{}, targetError(ctx, err)
	}
	return info, nil
}

func (l *lockedTarget) ScopeMax(ctx context.Context, schema, table, column string) (*int64, error) {
	var max *int64
	query := "SELECT max(" + pgx.Identifier{column}.Sanitize() + ")::bigint FROM " + pgx.Identifier{schema, table}.Sanitize()
	if err := l.conn.QueryRow(ctx, query).Scan(&max); err != nil {
		return nil, targetError(ctx, err)
	}
	return max, nil
}

func nullBatch(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}

func (l *lockedTarget) Checkpoints(ctx context.Context, executionID security.ID, direction targetbatch.Direction, query targetbatch.Query) ([]targetbatch.Checkpoint, error) {
	if !security.ValidID(executionID) || query.Limit < 1 || query.Limit > 1000 {
		return nil, security.ErrInvalidInput
	}
	rows, err := l.conn.Query(ctx, "SELECT batch_no, cursor_from, cursor_to, scope_max, rows, keys, final, recorded_at FROM tocatdz_internal.lookup_checkpoints($1,$2,$3,$4,$5,$6)",
		string(executionID), string(direction), nullBatch(query.AfterBatch), nullBatch(query.BeforeBatch), query.Limit, query.Descending)
	if err != nil {
		return nil, targetError(ctx, err)
	}
	defer rows.Close()
	var result []targetbatch.Checkpoint
	for rows.Next() {
		checkpoint := targetbatch.Checkpoint{ExecutionID: executionID, Direction: direction}
		if err := rows.Scan(&checkpoint.Batch, &checkpoint.From, &checkpoint.To, &checkpoint.ScopeMax, &checkpoint.Rows, &checkpoint.Keys,
			&checkpoint.Final, &checkpoint.RecordedAt); err != nil {
			return nil, targetError(ctx, err)
		}
		checkpoint.RecordedAt = checkpoint.RecordedAt.UTC()
		result = append(result, checkpoint)
	}
	if err := rows.Err(); err != nil {
		return nil, targetError(ctx, err)
	}
	return result, nil
}

func (l *lockedTarget) BeginBatch(ctx context.Context) (targetbatch.Tx, error) {
	tx, err := l.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return tx.(*targetTx), nil
}

func (t *targetTx) UpdateKeys(ctx context.Context, sql string, args ...any) ([]int64, error) {
	rows, err := t.tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, statementFailure(err)
	}
	defer rows.Close()
	var keys []int64
	for rows.Next() {
		var key int64
		if err := rows.Scan(&key); err != nil {
			return nil, statementFailure(err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, statementFailure(err)
	}
	return keys, nil
}

func (t *targetTx) WriteCheckpoint(ctx context.Context, checkpoint targetbatch.Checkpoint) error {
	keys := checkpoint.Keys
	if keys == nil {
		keys = []int64{}
	}
	checkpoint.Keys = keys
	_, err := t.tx.Exec(ctx, "SELECT tocatdz_internal.write_checkpoint($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)",
		string(checkpoint.ExecutionID), string(checkpoint.Direction), checkpoint.Batch, checkpoint.From, checkpoint.To, checkpoint.ScopeMax,
		checkpoint.Rows, keys, checkpoint.Final, t.box.checkpointProof(checkpoint, t.scope))
	if err != nil {
		return security.ErrUnavailable
	}
	return nil
}
