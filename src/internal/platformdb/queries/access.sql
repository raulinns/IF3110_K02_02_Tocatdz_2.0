-- name: ListDatabaseGrants :many
SELECT gda.access_level
FROM user_groups ug
JOIN group_database_access gda ON gda.group_id = ug.group_id
JOIN managed_databases md ON md.id = gda.database_id
WHERE ug.user_id = $1
  AND gda.database_id = $2
  AND md.active = TRUE
ORDER BY gda.access_level;

-- name: ListAccessibleDatabases :many
SELECT
    md.id,
    md.name,
    CASE MAX(
        CASE gda.access_level
            WHEN 'view_edit' THEN 2
            WHEN 'view_only' THEN 1
            ELSE 0
        END
    )
        WHEN 2 THEN 'view_edit'
        ELSE 'view_only'
    END::text AS access_level
FROM managed_databases md
JOIN group_database_access gda ON gda.database_id = md.id
JOIN user_groups ug ON ug.group_id = gda.group_id
WHERE ug.user_id = $1
  AND md.active = TRUE
GROUP BY md.id, md.name
ORDER BY lower(md.name), md.id;
