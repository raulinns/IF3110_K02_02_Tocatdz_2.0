CREATE TABLE users (
	id uuid PRIMARY KEY,
	username text NOT NULL UNIQUE CHECK (username ~ '^[a-z0-9_]{3,32}$'),
	password_hash text NOT NULL CHECK (octet_length(password_hash) BETWEEN 1 AND 512),
	active boolean NOT NULL DEFAULT true,
	platform_admin boolean NOT NULL DEFAULT false,
	auth_version bigint NOT NULL DEFAULT 1 CHECK (auth_version > 0),
	created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE groups (
	id uuid PRIMARY KEY,
	name text NOT NULL UNIQUE CHECK (char_length(name) BETWEEN 1 AND 64),
	created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE group_memberships (
	group_id uuid NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
	user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	PRIMARY KEY (group_id, user_id)
);

CREATE INDEX group_memberships_user_idx ON group_memberships (user_id);

-- Connection string disimpan terenkripsi (AES-256-GCM) dengan kunci di luar database.
CREATE TABLE managed_databases (
	id uuid PRIMARY KEY,
	name text NOT NULL UNIQUE CHECK (char_length(name) BETWEEN 1 AND 64),
	dsn_ciphertext bytea NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now()
);

-- Akses hanya lewat group; tidak ada tabel grant langsung ke user.
CREATE TABLE group_database_grants (
	group_id uuid NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
	database_id uuid NOT NULL REFERENCES managed_databases (id) ON DELETE CASCADE,
	access text NOT NULL CHECK (access IN ('view_only', 'view_edit')),
	PRIMARY KEY (group_id, database_id)
);

CREATE INDEX group_database_grants_database_idx ON group_database_grants (database_id);

CREATE TABLE sessions (
	id uuid PRIMARY KEY,
	user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	auth_version bigint NOT NULL CHECK (auth_version > 0),
	revision bigint NOT NULL CHECK (revision > 0),
	generation bigint NOT NULL CHECK (generation > 0),
	csrf_epoch bigint NOT NULL CHECK (csrf_epoch > 0),
	created_at timestamptz NOT NULL,
	last_seen_at timestamptz NOT NULL,
	absolute_expires_at timestamptz NOT NULL,
	rotated_at timestamptz NOT NULL,
	current_token_sealed text NOT NULL,
	previous_valid_until timestamptz,
	revoked_at timestamptz
);

CREATE INDEX sessions_user_idx ON sessions (user_id);

-- Satu tabel untuk digest current dan previous supaya keunikan berlaku lintas kedua slot.
CREATE TABLE session_tokens (
	digest bytea PRIMARY KEY CHECK (octet_length(digest) = 32),
	session_id uuid NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
	slot text NOT NULL CHECK (slot IN ('current', 'previous')),
	UNIQUE (session_id, slot)
);

CREATE TABLE migrations (
	id uuid PRIMARY KEY,
	database_id uuid NOT NULL REFERENCES managed_databases (id) ON DELETE CASCADE,
	title text NOT NULL,
	sql text NOT NULL,
	status text NOT NULL CHECK (status IN ('draft', 'pending', 'applied', 'failed')),
	version bigint CHECK (version > 0),
	revision bigint NOT NULL CHECK (revision > 0),
	execution_id uuid,
	checksum bytea CHECK (checksum IS NULL OR octet_length(checksum) = 32),
	actor_id uuid REFERENCES users (id),
	actor text,
	failure text NOT NULL DEFAULT '' CHECK (failure IN ('', 'sql_error', 'connection_error', 'interrupted')),
	executed_at timestamptz,
	created_by uuid NOT NULL REFERENCES users (id),
	created_at timestamptz NOT NULL,
	updated_at timestamptz NOT NULL,
	UNIQUE (database_id, version),
	CHECK ((status = 'draft') = (version IS NULL)),
	CHECK ((status = 'draft') = (execution_id IS NULL))
);

-- Riwayat linear: paling banyak satu eksekusi yang belum pasti per database target.
CREATE UNIQUE INDEX migrations_one_pending_idx ON migrations (database_id) WHERE status = 'pending';
CREATE INDEX migrations_history_idx ON migrations (database_id, version DESC NULLS FIRST, updated_at DESC, id);

-- SQL, versi, dan pelaksana migration yang sudah dieksekusi tidak boleh berubah (audit).
CREATE FUNCTION migrations_protect_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	IF TG_OP = 'DELETE' THEN
		IF OLD.status <> 'draft' THEN
			RAISE EXCEPTION 'executed migration is immutable' USING ERRCODE = 'check_violation';
		END IF;
		RETURN OLD;
	END IF;
	IF OLD.status <> 'draft' AND (
		NEW.sql IS DISTINCT FROM OLD.sql OR NEW.title IS DISTINCT FROM OLD.title OR
		NEW.version IS DISTINCT FROM OLD.version OR NEW.execution_id IS DISTINCT FROM OLD.execution_id OR
		NEW.checksum IS DISTINCT FROM OLD.checksum OR NEW.actor_id IS DISTINCT FROM OLD.actor_id OR
		NEW.executed_at IS DISTINCT FROM OLD.executed_at OR NEW.database_id IS DISTINCT FROM OLD.database_id OR
		OLD.status <> 'pending'
	) THEN
		RAISE EXCEPTION 'executed migration is immutable' USING ERRCODE = 'check_violation';
	END IF;
	RETURN NEW;
END;
$$;

CREATE TRIGGER migrations_protect_history
	BEFORE UPDATE OR DELETE ON migrations
	FOR EACH ROW EXECUTE FUNCTION migrations_protect_history();
