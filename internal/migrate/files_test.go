package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateLoadAndChecksum(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "migrations")
	for _, name := range []string{"first", "second"} {
		path, err := Create(dir, name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("-- +pfw Up\nSELECT 1;\n-- +pfw Down\nSELECT 1;\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := Load(dir)
	if err != nil || len(files) != 2 || files[0].Version != 1 || files[1].Version != 2 {
		t.Fatalf("files: %+v %v", files, err)
	}
	checksum := files[0].Checksum
	if err := os.WriteFile(filepath.Join(dir, "000001_first.sql"), []byte("-- +pfw Up\nSELECT 1;\n-- +pfw Down\nDROP TABLE first;\n"), 0600); err != nil {
		t.Fatal(err)
	}
	files, err = Load(dir)
	if err != nil || files[0].Checksum == checksum {
		t.Fatalf("down change not checksummed: %v", err)
	}
	if _, err := Create(dir, "../escape"); err == nil {
		t.Fatal("invalid name accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, ".pfw-create.lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(dir, "third"); err == nil {
		t.Fatal("concurrent creation lock ignored")
	}
}

func TestInvalidMigrationSets(t *testing.T) {
	for _, names := range [][]string{
		{"000001_first.up.sql"},
		{"000001_first.sql", "000001_second.sql"},
		{"000001_first.sql", "1_first.sql"},
		{"000000_zero.sql"},
		{"invalid.sql"},
	} {
		dir := t.TempDir()
		for _, name := range names {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("-- +pfw Up\nSELECT 1;\n-- +pfw Down\nSELECT 1;\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := Load(dir); err == nil {
			t.Fatalf("invalid set accepted: %v", names)
		}
	}
}

func TestTransactionControlValidation(t *testing.T) {
	if err := validateSQL("-- TODO write SQL\n/* nothing yet */"); err == nil {
		t.Fatal("unwritten migration accepted")
	}
	for _, sql := range []string{"BEGIN; SELECT 1; COMMIT;", "SELECT 1; /* nested /* x */ */ COMMIT;", "SELECT ';'; -- comment\nROLLBACK;", "DO $$BEGIN RAISE NOTICE 'ok'; END$$; END;", "PREPARE TRANSACTION 'x';", "START TRANSACTION;", `SELECT name'\'; COMMIT;`} {
		if err := validateSQL(sql); err == nil {
			t.Fatalf("transaction control accepted: %s", sql)
		}
	}
	for _, sql := range []string{"DO $body$ BEGIN RAISE NOTICE 'COMMIT;'; END $body$;", "CREATE TABLE items (name TEXT); INSERT INTO items VALUES ('rollback;');", `SELECT E'quote\'; COMMIT';`, "-- COMMIT;\nSELECT 1; /* ROLLBACK; */", `SELECT name'\';`, "CREATE TABLE foo$tag$bar (id INT);"} {
		if err := validateSQL(sql); err != nil {
			t.Fatalf("valid SQL rejected: %s %v", sql, err)
		}
	}
	if err := validateSQL("SELECT 'unterminated"); err == nil || !strings.Contains(err.Error(), "unterminated") {
		t.Fatal(err)
	}
}

func TestSectionsRejectMissingDuplicateAndUnscopedSQL(t *testing.T) {
	for _, source := range []string{
		"SELECT 1;",
		"-- +pfw Up\nSELECT 1;",
		"-- +pfw Down\nSELECT 1;\n-- +pfw Up\nSELECT 2;",
		"-- +pfw Up\nSELECT 1;\n-- +pfw Up\nSELECT 2;\n-- +pfw Down\nSELECT 3;",
		"SELECT 0;\n-- +pfw Up\nSELECT 1;\n-- +pfw Down\nSELECT 2;",
		"-- +pfw Up\nSELECT 1;\n-- +pfw Down\n-- TODO",
		"-- +pfw Up\nSELECT 1;\n-- +pfw Down\nCOMMIT;",
	} {
		if _, _, err := splitSections(source); err == nil {
			t.Fatalf("invalid sections accepted: %s", source)
		}
	}
}

func TestSectionMarkersInsideQuotedBodiesAreIgnored(t *testing.T) {
	source := "-- header\n-- +pfw Up\nDO $body$ BEGIN\n-- +pfw Down\nRAISE NOTICE 'ok'; END $body$;\nSELECT 'multiline\n-- +pfw Up\ntext';\n/*\n-- +pfw Down\n*/\n-- +pfw Down\nDROP TABLE items;\n"
	up, down, err := splitSections(source)
	if err != nil || !strings.Contains(up, "RAISE NOTICE") || down != "DROP TABLE items;\n" {
		t.Fatalf("split: up=%q down=%q err=%v", up, down, err)
	}
	up, down, err = splitSections("-- +pfw Up\r\nSELECT 1;\r\n-- +pfw Down\r\nSELECT 2;\r\n")
	if err != nil || up != "SELECT 1;\r\n" || down != "SELECT 2;\r\n" {
		t.Fatalf("CRLF: %q %q %v", up, down, err)
	}
}
