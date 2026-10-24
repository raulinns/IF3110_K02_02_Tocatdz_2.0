package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func (b *SecretBox) receiptSigningKey(scope security.ID) []byte {
	mac := hmac.New(sha256.New, []byte(b.receiptKey.Reveal()))
	_, _ = mac.Write([]byte("tocatdz/receipt/target/v1|" + string(scope)))
	return mac.Sum(nil)
}

func (b *SecretBox) receiptProof(key migration.ExecutionKey, scope security.ID) []byte {
	mac := hmac.New(sha256.New, b.receiptSigningKey(scope))
	_, _ = mac.Write([]byte(strings.Join([]string{"tocatdz/receipt/v1", string(key.DatabaseID), string(key.MigrationID), string(key.ExecutionID), strconv.FormatInt(key.Version, 10), hex.EncodeToString(key.Checksum[:])}, "|")))
	return mac.Sum(nil)
}
func (b *SecretBox) receiptFingerprint(scope security.ID) string {
	value := sha256.Sum256(b.receiptSigningKey(scope))
	return hex.EncodeToString(value[:])
}

func targetReady(ctx context.Context, conn *pgx.Conn, box *SecretBox) error {
	var unsafe bool
	err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_roles r WHERE
 (r.rolname=current_user OR pg_catalog.pg_has_role(current_user,r.oid,'MEMBER'))
 AND (r.rolsuper OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication OR r.rolbypassrls OR r.rolname IN ('pg_read_all_data','pg_write_all_data','pg_execute_server_program','pg_read_server_files','pg_write_server_files','pg_monitor','pg_read_all_stats')))`).Scan(&unsafe)
	if err != nil || unsafe {
		return security.ErrTargetNotReady
	}
	var version int
	var fingerprint, scope string
	if err := conn.QueryRow(ctx, "SELECT version,key_id,scope_id::text FROM tocatdz_internal.receipt_configuration()").Scan(&version, &fingerprint, &scope); err != nil || version != 1 || !security.ValidID(security.ID(scope)) || fingerprint != box.receiptFingerprint(security.ID(scope)) {
		return security.ErrTargetNotReady
	}
	var protected bool
	err = conn.QueryRow(ctx, `SELECT n.nspowner=c.relowner AND n.nspowner=f.proowner AND f.prosecdef AND NOT owner.rolcanlogin AND NOT owner.rolsuper
 AND NOT pg_catalog.pg_has_role(current_user,n.nspowner,'MEMBER')
 AND NOT pg_catalog.has_schema_privilege(current_user,n.oid,'CREATE')
 AND EXISTS(SELECT 1 FROM pg_catalog.pg_extension e WHERE e.extname='pgcrypto' AND e.extnamespace=n.oid)
 AND (SELECT bool_and(p.proowner=n.nspowner AND p.prosecdef) FROM pg_proc p WHERE p.oid IN ('tocatdz_internal.lookup_receipt(uuid)'::regprocedure,'tocatdz_internal.receipt_configuration()'::regprocedure))
 AND NOT pg_catalog.has_table_privilege(current_user,'tocatdz_internal.migration_receipts','SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER')
 AND NOT pg_catalog.has_table_privilege(current_user,'tocatdz_internal.receipt_keys','SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER')
 FROM pg_catalog.pg_namespace n JOIN pg_catalog.pg_roles owner ON owner.oid=n.nspowner
 JOIN pg_catalog.pg_class c ON c.relnamespace=n.oid AND c.relname='migration_receipts'
 JOIN pg_catalog.pg_proc f ON f.oid='tocatdz_internal.write_receipt(uuid,uuid,uuid,bigint,bytea,bytea)'::regprocedure
 WHERE n.nspname='tocatdz_internal'`).Scan(&protected)
	if err != nil || !protected {
		return security.ErrTargetNotReady
	}
	return nil
}

// PrepareManagedTarget menolak database metadata sebelum setup mengubah apa pun di target.
func PrepareManagedTarget(ctx context.Context, platform *pgxpool.Pool, dsn security.Secret, box *SecretBox) (security.Secret, error) {
	if platform == nil || box == nil {
		return security.Secret{}, security.ErrInvalidInput
	}
	cfg, err := targetConfig(dsn)
	if err != nil {
		return security.Secret{}, err
	}
	conn, err := pgx.ConnectConfig(ctx, cfg.ConnConfig)
	if err != nil {
		return security.Secret{}, security.ErrTargetNotReady
	}
	defer conn.Close(context.WithoutCancel(ctx))
	p, err := platform.Acquire(ctx)
	if err != nil {
		return security.Secret{}, security.ErrUnavailable
	}
	same, err := sameDatabase(ctx, conn, p.Conn())
	p.Release()
	if err != nil || same {
		return security.Secret{}, security.ErrTargetNotReady
	}
	return prepareTarget(ctx, dsn, box)
}

// DSN akun terbatas disimpan terenkripsi agar setup bisa diulang oleh pemilik database.
func prepareTarget(ctx context.Context, adminDSN security.Secret, box *SecretBox) (security.Secret, error) {
	u, err := url.Parse(adminDSN.Reveal())
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || box == nil {
		return security.Secret{}, security.ErrTargetNotReady
	}
	conn, err := pgx.Connect(ctx, adminDSN.Reveal())
	if err != nil {
		return security.Secret{}, security.ErrTargetNotReady
	}
	defer conn.Close(context.WithoutCancel(ctx))
	if targetReady(ctx, conn, box) == nil {
		return adminDSN, nil
	}
	var privileged bool
	var oid uint32
	if err := conn.QueryRow(ctx, "SELECT rolsuper FROM pg_catalog.pg_roles WHERE rolname=current_user").Scan(&privileged); err != nil || !privileged {
		return security.Secret{}, security.ErrTargetNotReady
	}
	if err := conn.QueryRow(ctx, "SELECT oid FROM pg_catalog.pg_database WHERE datname=current_database()").Scan(&oid); err != nil {
		return security.Secret{}, security.ErrTargetNotReady
	}
	owner := "tocatdz_receipts_" + strconv.FormatUint(uint64(oid), 10)
	runtime := "tocatdz_exec_" + strconv.FormatUint(uint64(oid), 10)
	tx, err := conn.Begin(ctx)
	if err != nil {
		return security.Secret{}, security.ErrUnavailable
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err := tx.Exec(ctx, "SELECT pg_catalog.pg_advisory_xact_lock($1)", int64(targetLockKey)); err != nil {
		return security.Secret{}, security.ErrUnavailable
	}
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT to_regclass('tocatdz_internal.receipt_keys') IS NOT NULL").Scan(&exists); err != nil {
		return security.Secret{}, security.ErrTargetNotReady
	}
	if exists {
		var id, key string
		var sealed []byte
		if err := tx.QueryRow(ctx, "SELECT runtime_id::text, runtime_secret, encode(sha256(key),'hex') FROM tocatdz_internal.receipt_keys WHERE singleton").Scan(&id, &sealed, &key); err != nil || !security.ValidID(security.ID(id)) || key != box.receiptFingerprint(security.ID(id)) {
			return security.Secret{}, security.ErrTargetNotReady
		}
		dsn, err := box.Open(security.ID(id), sealed)
		if err != nil {
			return security.Secret{}, security.ErrTargetNotReady
		}
		parsed, _ := url.Parse(dsn.Reveal())
		if parsed == nil || parsed.Host != u.Host || parsed.Path != u.Path {
			return security.Secret{}, security.ErrTargetNotReady
		}
		return dsn, nil
	}
	for _, role := range []string{owner, runtime} {
		var present bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)", role).Scan(&present); err != nil || present {
			return security.Secret{}, security.ErrTargetNotReady
		}
	}
	password, err := security.NewTokenGenerator().Generate()
	if err != nil {
		return security.Secret{}, err
	}
	var createRole string
	if err := tx.QueryRow(ctx, "SELECT format('CREATE ROLE %I LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS NOINHERIT PASSWORD %L',$1::text,$2::text)", runtime, password.Reveal()).Scan(&createRole); err != nil {
		return security.Secret{}, security.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, "CREATE ROLE "+pgx.Identifier{owner}.Sanitize()+" NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS NOINHERIT"); err != nil {
		return security.Secret{}, security.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, createRole); err != nil {
		return security.Secret{}, security.ErrUnavailable
	}
	var database string
	if err := tx.QueryRow(ctx, "SELECT current_database()").Scan(&database); err != nil {
		return security.Secret{}, security.ErrUnavailable
	}
	for _, sql := range []string{
		"GRANT CONNECT,CREATE ON DATABASE " + pgx.Identifier{database}.Sanitize() + " TO " + pgx.Identifier{runtime}.Sanitize(),
		"GRANT USAGE,CREATE ON SCHEMA public TO " + pgx.Identifier{runtime}.Sanitize(),
		"CREATE SCHEMA IF NOT EXISTS tocatdz_internal AUTHORIZATION " + pgx.Identifier{owner}.Sanitize(),
		"CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA tocatdz_internal",
		`DO $setup$ BEGIN IF EXISTS(SELECT 1 FROM pg_catalog.pg_extension WHERE extname='pgcrypto' AND extnamespace<>'tocatdz_internal'::regnamespace) THEN ALTER EXTENSION pgcrypto SET SCHEMA tocatdz_internal; END IF; END $setup$`,
		`CREATE TABLE IF NOT EXISTS tocatdz_internal.migration_receipts (execution_id uuid PRIMARY KEY,database_id uuid NOT NULL,migration_id uuid NOT NULL,version bigint NOT NULL CHECK(version>0),checksum bytea NOT NULL CHECK(octet_length(checksum)=32),recorded_at timestamptz NOT NULL DEFAULT now())`,
		`CREATE TABLE tocatdz_internal.receipt_keys (singleton boolean PRIMARY KEY CHECK(singleton),key bytea NOT NULL,runtime_id uuid NOT NULL,runtime_secret bytea NOT NULL)`,
		"ALTER TABLE tocatdz_internal.migration_receipts OWNER TO " + pgx.Identifier{owner}.Sanitize(),
		"ALTER TABLE tocatdz_internal.receipt_keys OWNER TO " + pgx.Identifier{owner}.Sanitize(),
		"ALTER SCHEMA tocatdz_internal OWNER TO " + pgx.Identifier{owner}.Sanitize(),
	} {
		if _, err := tx.Exec(ctx, sql); err != nil {
			return security.Secret{}, security.ErrTargetNotReady
		}
	}
	runtimeURL := *u
	runtimeURL.User = url.UserPassword(runtime, password.Reveal())
	query := runtimeURL.Query()
	for name := range query {
		if strings.EqualFold(name, "user") || strings.EqualFold(name, "password") {
			query.Del(name)
		}
	}
	runtimeURL.RawQuery = query.Encode()
	runtimeDSN := security.NewSecret(runtimeURL.String())
	id, err := security.NewTokenGenerator().GenerateID()
	if err != nil {
		return security.Secret{}, err
	}
	sealed, err := box.Seal(id, runtimeDSN)
	if err != nil {
		return security.Secret{}, err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO tocatdz_internal.receipt_keys VALUES(true,$1,$2,$3)", box.receiptSigningKey(id), string(id), sealed); err != nil {
		return security.Secret{}, security.ErrUnavailable
	}
	writer := `CREATE OR REPLACE FUNCTION tocatdz_internal.write_receipt(d uuid,m uuid,e uuid,v bigint,c bytea,p bytea) RETURNS void
 LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,tocatdz_internal,pg_temp AS $body$
 DECLARE secret bytea; expected bytea;
 BEGIN
 SELECT key INTO STRICT secret FROM tocatdz_internal.receipt_keys WHERE singleton;
 expected := tocatdz_internal.hmac(convert_to(concat_ws('|','tocatdz/receipt/v1',d::text,m::text,e::text,v::text,encode(c,'hex')),'UTF8'),secret,'sha256');
 IF d IS NULL OR m IS NULL OR e IS NULL OR v IS NULL OR v<=0 OR c IS NULL OR octet_length(c)<>32 OR p IS NULL OR p<>expected THEN RAISE EXCEPTION 'invalid receipt proof' USING ERRCODE='42501'; END IF;
 INSERT INTO tocatdz_internal.migration_receipts(execution_id,database_id,migration_id,version,checksum) VALUES(e,d,m,v,c);
	 END $body$`
	for _, sql := range []string{writer,
		`CREATE OR REPLACE FUNCTION tocatdz_internal.receipt_configuration() RETURNS TABLE(version int,key_id text,scope_id uuid) LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,tocatdz_internal,pg_temp AS 'SELECT 1, encode(sha256(key),''hex''), runtime_id FROM tocatdz_internal.receipt_keys WHERE singleton'`,
		`CREATE OR REPLACE FUNCTION tocatdz_internal.lookup_receipt(e uuid) RETURNS TABLE(database_id uuid,migration_id uuid,execution_id uuid,version bigint,checksum bytea) LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,tocatdz_internal,pg_temp AS 'SELECT database_id,migration_id,execution_id,version,checksum FROM tocatdz_internal.migration_receipts WHERE execution_id=e'`,
	} {
		if _, err := tx.Exec(ctx, sql); err != nil {
			return security.Secret{}, security.ErrUnavailable
		}
	}
	for _, signature := range []string{"write_receipt(uuid,uuid,uuid,bigint,bytea,bytea)", "receipt_configuration()", "lookup_receipt(uuid)"} {
		for _, sql := range []string{"ALTER FUNCTION tocatdz_internal." + signature + " OWNER TO " + pgx.Identifier{owner}.Sanitize(), "REVOKE ALL ON FUNCTION tocatdz_internal." + signature + " FROM PUBLIC", "GRANT EXECUTE ON FUNCTION tocatdz_internal." + signature + " TO " + pgx.Identifier{runtime}.Sanitize()} {
			if _, err := tx.Exec(ctx, sql); err != nil {
				return security.Secret{}, security.ErrUnavailable
			}
		}
	}
	for _, sql := range []string{"REVOKE ALL ON ALL TABLES IN SCHEMA tocatdz_internal FROM PUBLIC," + pgx.Identifier{runtime}.Sanitize(), "REVOKE ALL ON SCHEMA tocatdz_internal FROM PUBLIC," + pgx.Identifier{runtime}.Sanitize(), "GRANT USAGE ON SCHEMA tocatdz_internal TO " + pgx.Identifier{runtime}.Sanitize()} {
		if _, err := tx.Exec(ctx, sql); err != nil {
			return security.Secret{}, security.ErrUnavailable
		}
	}
	schemas, err := tx.Query(ctx, `SELECT n.nspname FROM pg_namespace n WHERE `+hiddenSchemas+` AND n.nspowner=(SELECT oid FROM pg_roles WHERE rolname=current_user)`)
	if err != nil {
		return security.Secret{}, security.ErrUnavailable
	}
	var ownedSchemas []string
	for schemas.Next() {
		var name string
		if schemas.Scan(&name) != nil {
			schemas.Close()
			return security.Secret{}, security.ErrUnavailable
		}
		ownedSchemas = append(ownedSchemas, name)
	}
	err = schemas.Err()
	schemas.Close()
	if err != nil {
		return security.Secret{}, security.ErrUnavailable
	}
	for _, name := range ownedSchemas {
		if _, err := tx.Exec(ctx, "ALTER SCHEMA "+pgx.Identifier{name}.Sanitize()+" OWNER TO "+pgx.Identifier{runtime}.Sanitize()); err != nil {
			return security.Secret{}, security.ErrUnavailable
		}
	}
	rows, err := tx.Query(ctx, `SELECT c.relkind::text,n.nspname,c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE `+hiddenSchemas+` AND c.relkind IN ('r','p','v','m','S') AND c.relowner=(SELECT oid FROM pg_roles WHERE rolname=current_user) AND NOT EXISTS(SELECT 1 FROM pg_depend d WHERE d.classid='pg_class'::regclass AND d.objid=c.oid AND d.deptype='e') ORDER BY CASE WHEN c.relkind='S' THEN 1 ELSE 0 END`)
	if err != nil {
		return security.Secret{}, security.ErrUnavailable
	}
	type relation struct{ kind, schema, name string }
	var relations []relation
	for rows.Next() {
		var r relation
		if rows.Scan(&r.kind, &r.schema, &r.name) != nil {
			rows.Close()
			return security.Secret{}, security.ErrUnavailable
		}
		relations = append(relations, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return security.Secret{}, security.ErrUnavailable
	}
	for _, r := range relations {
		kind := "TABLE"
		switch r.kind {
		case "S":
			kind = "SEQUENCE"
		case "v":
			kind = "VIEW"
		case "m":
			kind = "MATERIALIZED VIEW"
		}
		if _, err := tx.Exec(ctx, "ALTER "+kind+" "+pgx.Identifier{r.schema, r.name}.Sanitize()+" OWNER TO "+pgx.Identifier{runtime}.Sanitize()); err != nil {
			return security.Secret{}, security.ErrUnavailable
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return security.Secret{}, security.ErrUnavailable
	}
	return runtimeDSN, nil
}

func sameDatabase(ctx context.Context, a, b *pgx.Conn) (bool, error) {
	var aID, bID uint32
	var aStart, bStart string
	query := `SELECT oid,pg_catalog.pg_postmaster_start_time()::text FROM pg_catalog.pg_database WHERE datname=current_database()`
	if err := a.QueryRow(ctx, query).Scan(&aID, &aStart); err != nil {
		return false, err
	}
	if err := b.QueryRow(ctx, query).Scan(&bID, &bStart); err != nil {
		return false, err
	}
	return aID == bID && aStart == bStart, nil
}

func sameTargetDSN(ctx context.Context, a, b security.Secret) (bool, error) {
	aCfg, err := targetConfig(a)
	if err != nil {
		return false, err
	}
	bCfg, err := targetConfig(b)
	if err != nil {
		return false, err
	}
	aConn, err := pgx.ConnectConfig(ctx, aCfg.ConnConfig)
	if err != nil {
		return false, security.ErrUnavailable
	}
	defer aConn.Close(context.WithoutCancel(ctx))
	bConn, err := pgx.ConnectConfig(ctx, bCfg.ConnConfig)
	if err != nil {
		return false, security.ErrUnavailable
	}
	defer bConn.Close(context.WithoutCancel(ctx))
	return sameDatabase(ctx, aConn, bConn)
}
