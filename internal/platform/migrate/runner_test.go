package migrate

import "testing"

func TestSplitStatements(t *testing.T) {
	body := `
		-- setup
		CREATE TABLE test (value VARCHAR(32));
		INSERT INTO test(value) VALUES ('a;b'); # trailing comment
		/* ignored ; */ INSERT INTO test(value) VALUES ("c;d");
	`
	statements := splitStatements(body)
	if len(statements) != 3 {
		t.Fatalf("len(statements) = %d, statements = %#v", len(statements), statements)
	}
}
