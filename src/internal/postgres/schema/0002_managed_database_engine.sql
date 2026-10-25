ALTER TABLE managed_databases
	ADD COLUMN engine text NOT NULL DEFAULT 'postgresql'
	CHECK (engine IN ('postgresql', 'mysql'));
