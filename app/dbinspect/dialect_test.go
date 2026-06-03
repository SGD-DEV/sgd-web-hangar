package dbinspect

import (
	"strings"
	"testing"
)

// TestDialectHelpersBranchCorrectly is a quick smoke test that every
// DDL helper actually emits different SQL for MySQL vs Postgres - the
// whole point of the dialect parameter. Catches regressions like
// "someone copy-pasted the PG branch into MySQL and forgot to edit."
func TestDialectHelpersBranchCorrectly(t *testing.T) {
	cases := []struct {
		name string
		pg   string
		my   string
		mustDifferOn string // substring expected only in MySQL output
	}{
		{
			name:         "surrogate id",
			pg:           ddlAddSurrogateID(DialectPostgres, "users"),
			my:           ddlAddSurrogateID(DialectMySQL, "users"),
			mustDifferOn: "AUTO_INCREMENT",
		},
		{
			name:         "money decimal",
			pg:           ddlMoneyDecimal(DialectPostgres, "orders", "amount"),
			my:           ddlMoneyDecimal(DialectMySQL, "orders", "amount"),
			mustDifferOn: "MODIFY COLUMN",
		},
		{
			name:         "to bigint",
			pg:           ddlToBigInt(DialectPostgres, "sales", "id"),
			my:           ddlToBigInt(DialectMySQL, "sales", "id"),
			mustDifferOn: "MODIFY COLUMN",
		},
		{
			name:         "to boolean",
			pg:           ddlToBoolean(DialectPostgres, "users", "is_admin"),
			my:           ddlToBoolean(DialectMySQL, "users", "is_admin"),
			mustDifferOn: "TINYINT",
		},
		{
			name:         "create_at",
			pg:           ddlAddCreatedAt(DialectPostgres, "events"),
			my:           ddlAddCreatedAt(DialectMySQL, "events"),
			mustDifferOn: "CURRENT_TIMESTAMP",
		},
		{
			name:         "phone varchar",
			pg:           ddlPhoneToVarchar(DialectPostgres, "users", "phone"),
			my:           ddlPhoneToVarchar(DialectMySQL, "users", "phone"),
			mustDifferOn: "MODIFY COLUMN",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.pg == c.my {
				t.Errorf("dialects produced identical SQL:\n  %s", c.pg)
			}
			if !strings.Contains(c.my, c.mustDifferOn) {
				t.Errorf("MySQL output missing expected token %q:\n  %s", c.mustDifferOn, c.my)
			}
		})
	}
}

// TestDialectFromDBType verifies the auto-detect maps the db_type
// strings we accept (mysql / mariadb / postgresql / postgres / pg)
// to the right Dialect. Anything unknown falls back to Postgres.
func TestDialectFromDBType(t *testing.T) {
	cases := map[string]Dialect{
		"mysql":      DialectMySQL,
		"MySQL":      DialectMySQL,
		"mariadb":    DialectMySQL,
		"MariaDB":    DialectMySQL,
		"postgresql": DialectPostgres,
		"postgres":   DialectPostgres,
		"pg":         DialectPostgres,
		"":           DialectPostgres,
		"sqlite":     DialectPostgres, // fall-through
	}
	for in, want := range cases {
		if got := dialectFromDBType(in); got != want {
			t.Errorf("dialectFromDBType(%q) = %q, want %q", in, got, want)
		}
	}
}
