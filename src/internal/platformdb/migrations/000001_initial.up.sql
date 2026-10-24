CREATE TABLE users (
    id UUID PRIMARY KEY,
    username VARCHAR(32) NOT NULL,
    password_hash TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    platform_admin BOOLEAN NOT NULL DEFAULT FALSE,
    auth_version BIGINT NOT NULL DEFAULT 1 CHECK (auth_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT users_username_canonical CHECK (
        username = lower(btrim(username))
        AND username ~ '^[a-z0-9_]{3,32}$'
    ),
    CONSTRAINT users_username_unique UNIQUE (username)
);

CREATE TABLE groups (
    id UUID PRIMARY KEY,
    name VARCHAR(80) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT groups_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT groups_name_unique UNIQUE (name)
);

CREATE TABLE user_groups (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, group_id)
);

CREATE TABLE managed_databases (
    id UUID PRIMARY KEY,
    name VARCHAR(120) NOT NULL,
    engine VARCHAR(20) NOT NULL,
    connection_secret BYTEA NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT managed_databases_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT managed_databases_name_unique UNIQUE (name),
    CONSTRAINT managed_databases_engine_valid CHECK (
        engine IN ('postgresql', 'mysql')
    ),
    CONSTRAINT managed_databases_secret_not_empty CHECK (
        octet_length(connection_secret) > 0
    )
);

CREATE TABLE group_database_access (
    group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    database_id UUID NOT NULL REFERENCES managed_databases(id) ON DELETE CASCADE,
    access_level VARCHAR(20) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (group_id, database_id),
    CONSTRAINT group_database_access_level_valid CHECK (
        access_level IN ('view_only', 'view_edit')
    )
);

CREATE TABLE sessions (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    auth_version BIGINT NOT NULL CHECK (auth_version > 0),
    revision BIGINT NOT NULL CHECK (revision > 0),
    generation BIGINT NOT NULL CHECK (generation > 0),
    csrf_epoch BIGINT NOT NULL CHECK (csrf_epoch > 0),
    created_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    absolute_expires_at TIMESTAMPTZ NOT NULL,
    rotated_at TIMESTAMPTZ NOT NULL,
    current_digest BYTEA NOT NULL,
    previous_digest BYTEA,
    previous_valid_until TIMESTAMPTZ,
    current_token_sealed TEXT NOT NULL,
    revoked_at TIMESTAMPTZ,
    CONSTRAINT sessions_current_digest_size CHECK (
        octet_length(current_digest) = 32
    ),
    CONSTRAINT sessions_previous_digest_size CHECK (
        previous_digest IS NULL OR octet_length(previous_digest) = 32
    ),
    CONSTRAINT sessions_previous_pair CHECK (
        (previous_digest IS NULL) = (previous_valid_until IS NULL)
    ),
    CONSTRAINT sessions_time_order CHECK (
        created_at <= last_seen_at
        AND created_at <= rotated_at
        AND created_at < absolute_expires_at
    ),
    CONSTRAINT sessions_current_digest_unique UNIQUE (current_digest)
);

CREATE UNIQUE INDEX sessions_previous_digest_unique
    ON sessions (previous_digest)
    WHERE previous_digest IS NOT NULL;

CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_absolute_expiry_idx ON sessions (absolute_expires_at);

CREATE TABLE migration_history (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    database_id UUID NOT NULL REFERENCES managed_databases(id) ON DELETE CASCADE,
    version BIGINT NOT NULL CHECK (version > 0),
    sql_text TEXT NOT NULL,
    executed_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    executed_at TIMESTAMPTZ,
    error_message TEXT,
    CONSTRAINT migration_history_sql_not_blank CHECK (btrim(sql_text) <> ''),
    CONSTRAINT migration_history_status_valid CHECK (
        status IN ('pending', 'running', 'committed', 'rolled_back', 'failed')
    ),
    CONSTRAINT migration_history_execution_state CHECK (
        (status = 'pending' AND executed_at IS NULL)
        OR (status <> 'pending' AND executed_at IS NOT NULL)
    ),
    CONSTRAINT migration_history_version_unique UNIQUE (database_id, version)
);

CREATE INDEX migration_history_database_created_idx
    ON migration_history (database_id, created_at DESC);
