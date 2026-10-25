package migration

import (
	"context"
	"errors"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestMySQLValidatorAcceptsSchemaAndDataMigrations(t *testing.T) {
	t.Parallel()
	validator := NewMySQLValidator()
	for _, statement := range []string{
		"CREATE TABLE `orders` (`id` BIGINT AUTO_INCREMENT PRIMARY KEY, `total` DECIMAL(12,2) NOT NULL);",
		"ALTER TABLE `orders` ADD COLUMN `note` VARCHAR(255); UPDATE `orders` SET `note` = 'paid; safely';",
		"CREATE INDEX `orders_total_idx` ON `orders` (`total`);",
		"CREATE UNIQUE INDEX `orders_note_idx` ON `orders` (`note`);",
		"INSERT INTO `orders` (`total`) VALUES (10.50); DELETE FROM `orders` WHERE `id` = 99;",
	} {
		if err := validator.Validate(context.Background(), security.NewSecret(statement)); err != nil {
			t.Fatalf("Validate(%q) returned %v", statement, err)
		}
	}
}

func TestMySQLValidatorRejectsUnsafeOrMalformedMigrations(t *testing.T) {
	t.Parallel()
	validator := NewMySQLValidator()
	for _, statement := range []string{
		"START TRANSACTION; CREATE TABLE x (id INT); COMMIT;",
		"DROP DATABASE production;",
		"CREATE USER attacker IDENTIFIED BY 'secret';",
		"SELECT * FROM information_schema.tables;",
		"DELETE FROM `_tocatdz_migration_receipts`;",
		"LOAD DATA LOCAL INFILE '/tmp/x' INTO TABLE x;",
		"/*! DROP TABLE orders */;",
		"CREATE TABLE `unterminated (id INT);",
	} {
		err := validator.Validate(context.Background(), security.NewSecret(statement))
		if !errors.Is(err, security.ErrInvalidInput) {
			t.Fatalf("Validate(%q) error = %v, want invalid input", statement, err)
		}
	}
}
