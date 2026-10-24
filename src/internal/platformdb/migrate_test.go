package platformdb

import (
	"testing"
	"testing/fstest"
)

func TestLoadMigrationsSortsVersionsAndComputesChecksums(t *testing.T) {
	files := fstest.MapFS{
		"migrations/000002_second.up.sql":  {Data: []byte("CREATE TABLE second (id bigint);")},
		"migrations/000001_first.up.sql":   {Data: []byte("CREATE TABLE first (id bigint);")},
		"migrations/000001_first.down.sql": {Data: []byte("DROP TABLE first;")},
	}

	migrations, err := loadMigrations(files)
	if err != nil {
		t.Fatalf("loadMigrations() returned error: %v", err)
	}
	if len(migrations) != 2 || migrations[0].Version != 1 || migrations[1].Version != 2 {
		t.Fatalf("migration order = %#v, want versions 1 then 2", migrations)
	}
	if migrations[0].Name != "000001_first.up.sql" || migrations[0].Checksum == "" {
		t.Fatal("migration metadata was not derived from the file")
	}
}

func TestLoadMigrationsRejectsInvalidOrDuplicateVersions(t *testing.T) {
	tests := []struct {
		name  string
		files fstest.MapFS
	}{
		{
			name: "invalid filename",
			files: fstest.MapFS{
				"migrations/initial.up.sql": {Data: []byte("SELECT 1;")},
			},
		},
		{
			name: "duplicate version",
			files: fstest.MapFS{
				"migrations/000001_first.up.sql":  {Data: []byte("SELECT 1;")},
				"migrations/000001_second.up.sql": {Data: []byte("SELECT 2;")},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := loadMigrations(test.files); err == nil {
				t.Fatal("loadMigrations() accepted invalid migration set")
			}
		})
	}
}

func TestEmbeddedPlatformMigrationsAreValid(t *testing.T) {
	migrations, err := loadMigrations(embeddedMigrations)
	if err != nil {
		t.Fatalf("embedded migrations are invalid: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("no platform migrations were embedded")
	}
}
