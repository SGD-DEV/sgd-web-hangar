// Package dbinspect runs ad-hoc SQL queries against MySQL/PostgreSQL and
// produces a structured schema dump + a server-side audit that emits a
// runnable .sql migration file an MCP-connected AI agent can hand
// straight to the developer.
//
// The audit pipeline is:
//
//	 1. Inspect()        - dump tables, columns, indexes, FKs, triggers
//	                       into a Schema struct (see this file).
//	 2. Audit()          - walk the schema with ~30 deterministic rules
//	                       and produce []Finding (see audit.go).
//	 3. WriteFixesSQL()  - serialise the findings to a .sql file with
//	                       severity-grouped sections, each fix preceded
//	                       by a `-- [SEVERITY] table.column: description`
//	                       comment block (see audit.go).
//
// Hangar's MCP server returns the file path + an AuditSummary
// (count-by-severity) + a tiny BuildAuditSummaryPrompt so the agent
// knows what to do next. The full Schema JSON is only included on
// request (the audit logic doesn't need to round-trip through the agent
// any more).
package dbinspect

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql" // mysql driver
	_ "github.com/lib/pq"              // postgres driver
)

// QueryResult is the wire format for ad-hoc SQL execution. Columns and Rows
// are populated for SELECTs; RowsAffected for DML. ExecTime measures the
// driver round-trip so the AI agent can spot slow queries.
type QueryResult struct {
	Columns      []string                 `json:"columns,omitempty"`
	Rows         []map[string]interface{} `json:"rows,omitempty"`
	RowsAffected int64                    `json:"rows_affected,omitempty"`
	ExecMillis   int64                    `json:"exec_ms"`
	Truncated    bool                     `json:"truncated,omitempty"` // result set capped at maxRows
}

// Schema is the structured database snapshot returned by Inspect. Designed
// for both human reading and JSON serialization to an LLM context. Field
// names match the keys the BuildAuditPrompt prompt references so the
// agent's "single source of truth" actually contains what it expects.
type Schema struct {
	DBType           string             `json:"db_type"`
	DBName           string             `json:"db_name"`
	Tables           []TableInfo        `json:"tables"`
	ForeignKeys      []ForeignKey       `json:"foreign_keys"`
	Triggers         []TriggerInfo      `json:"triggers,omitempty"`
	Views            []ViewInfo         `json:"views,omitempty"`
	StoredProcedures []StoredProcedure  `json:"stored_procedures,omitempty"`
	InspectedAt      string             `json:"inspected_at"`
	TableCount       int                `json:"table_count"`
	TotalColumns     int                `json:"total_columns"`
}

type ViewInfo struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`           // CREATE VIEW body
	Security   string `json:"security,omitempty"`   // DEFINER / INVOKER (MySQL); INVOKER / DEFINER (Postgres SECURITY)
	Definer    string `json:"definer,omitempty"`
}

type StoredProcedure struct {
	Name       string `json:"name"`
	Type       string `json:"type"`                   // PROCEDURE | FUNCTION
	Language   string `json:"language,omitempty"`     // SQL / plpgsql / etc.
	Definition string `json:"definition"`             // body
	Security   string `json:"security,omitempty"`     // DEFINER / INVOKER
	Definer    string `json:"definer,omitempty"`
	Parameters string `json:"parameters,omitempty"`   // raw parameter list
}

type TableInfo struct {
	Name       string       `json:"name"`
	Columns    []ColumnInfo `json:"columns"`
	Indexes    []IndexInfo  `json:"indexes,omitempty"`
	PrimaryKey []string     `json:"primary_key,omitempty"`
	RowCount   int64        `json:"row_count_approx,omitempty"` // best-effort
}

type ColumnInfo struct {
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	Nullable     bool    `json:"nullable"`
	Default      *string `json:"default,omitempty"`
	IsPrimaryKey bool    `json:"is_pk,omitempty"`
	Comment      string  `json:"comment,omitempty"`
}

type IndexInfo struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
}

type ForeignKey struct {
	Table         string `json:"table"`
	Column        string `json:"column"`
	ReferencedDB  string `json:"referenced_db,omitempty"`
	ReferencedTbl string `json:"referenced_table"`
	ReferencedCol string `json:"referenced_column"`
	OnDelete      string `json:"on_delete,omitempty"`
	OnUpdate      string `json:"on_update,omitempty"`
}

type TriggerInfo struct {
	Name      string `json:"name"`
	Table     string `json:"table"`
	Event     string `json:"event"`     // INSERT/UPDATE/DELETE
	Timing    string `json:"timing"`    // BEFORE/AFTER
	Statement string `json:"statement"` // SQL body, may be long
}

// Cap on returned rows. SELECT * FROM big_table shouldn't blow up the MCP
// channel. Agents that need full data can paginate with LIMIT/OFFSET.
const maxRows = 500

// RunQuery executes sql against the given DB and returns columns+rows for
// SELECTs or RowsAffected for DML. Caller decides safety - this is meant
// for local-dev DBs only, exposed only via the local MCP server.
func RunQuery(dbType, dsn, sqlText string) (*QueryResult, error) {
	driver, err := driverName(dbType)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", dbType, err)
	}
	defer db.Close()

	// Tight timeouts - dev DB on localhost should respond fast; if it doesn't,
	// we'd rather error out than hang the MCP request.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	trimmed := strings.TrimSpace(strings.ToUpper(sqlText))
	isSelect := strings.HasPrefix(trimmed, "SELECT") || strings.HasPrefix(trimmed, "SHOW") ||
		strings.HasPrefix(trimmed, "EXPLAIN") || strings.HasPrefix(trimmed, "WITH") ||
		strings.HasPrefix(trimmed, "DESCRIBE") || strings.HasPrefix(trimmed, "DESC ")

	if isSelect {
		rows, err := db.QueryContext(ctx, sqlText)
		if err != nil {
			return nil, fmt.Errorf("query: %w", err)
		}
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			return nil, fmt.Errorf("columns: %w", err)
		}
		out := &QueryResult{Columns: cols}
		count := 0
		for rows.Next() {
			if count >= maxRows {
				out.Truncated = true
				break
			}
			row := make(map[string]interface{}, len(cols))
			scanTargets := make([]interface{}, len(cols))
			rawValues := make([]interface{}, len(cols))
			for i := range cols {
				scanTargets[i] = &rawValues[i]
			}
			if err := rows.Scan(scanTargets...); err != nil {
				return nil, fmt.Errorf("scan: %w", err)
			}
			for i, col := range cols {
				row[col] = normalizeValue(rawValues[i])
			}
			out.Rows = append(out.Rows, row)
			count++
		}
		out.ExecMillis = time.Since(start).Milliseconds()
		return out, nil
	}

	res, err := db.ExecContext(ctx, sqlText)
	if err != nil {
		return nil, fmt.Errorf("exec: %w", err)
	}
	affected, _ := res.RowsAffected()
	return &QueryResult{RowsAffected: affected, ExecMillis: time.Since(start).Milliseconds()}, nil
}

// normalizeValue makes raw driver values JSON-friendly. []byte from MySQL
// is usually a string; time.Time becomes RFC3339.
func normalizeValue(v interface{}) interface{} {
	switch x := v.(type) {
	case []byte:
		return string(x)
	case time.Time:
		return x.Format(time.RFC3339)
	}
	return v
}

func driverName(dbType string) (string, error) {
	switch strings.ToLower(dbType) {
	case "mysql", "mariadb":
		return "mysql", nil
	case "postgresql", "postgres", "pg":
		return "postgres", nil
	}
	return "", fmt.Errorf("dbinspect: unknown db type %q (want mysql or postgres)", dbType)
}

// BuildDSN returns a driver-compatible DSN for the given local Hangar DB.
func BuildDSN(dbType, host string, port int, user, password, database string) string {
	switch strings.ToLower(dbType) {
	case "mysql", "mariadb":
		// user:pass@tcp(host:port)/db?parseTime=true
		auth := user
		if password != "" {
			auth = user + ":" + password
		}
		dsn := fmt.Sprintf("%s@tcp(%s:%d)/", auth, host, port)
		if database != "" {
			dsn += database
		}
		return dsn + "?parseTime=true&multiStatements=true"
	case "postgresql", "postgres", "pg":
		// host=... port=... user=... dbname=... sslmode=disable
		parts := []string{
			"host=" + host,
			fmt.Sprintf("port=%d", port),
			"user=" + user,
			"sslmode=disable",
		}
		if password != "" {
			parts = append(parts, "password="+password)
		}
		if database != "" {
			parts = append(parts, "dbname="+database)
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// Inspect dumps tables, columns, indexes, FKs, triggers from the given DB.
// Uses INFORMATION_SCHEMA for portability across MySQL/MariaDB and the
// pg_catalog views for PostgreSQL.
func Inspect(dbType, dsn, dbName string) (*Schema, error) {
	driver, err := driverName(dbType)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("opening: %w", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	schema := &Schema{
		DBType:      dbType,
		DBName:      dbName,
		InspectedAt: time.Now().Format(time.RFC3339),
	}

	switch driver {
	case "mysql":
		if err := inspectMySQL(ctx, db, dbName, schema); err != nil {
			return nil, err
		}
	case "postgres":
		if err := inspectPostgres(ctx, db, dbName, schema); err != nil {
			return nil, err
		}
	}

	schema.TableCount = len(schema.Tables)
	for _, t := range schema.Tables {
		schema.TotalColumns += len(t.Columns)
	}
	return schema, nil
}

// BuildAuditSummaryPrompt is what the MCP tool returns to the agent
// alongside the SQL file path + counts. It is intentionally short: the
// real work happened server-side in Audit() + WriteFixesSQL(). The agent
// only needs to read the file, group it for the user, and (optionally)
// help apply the fixes.
//
// projectContext (optional) is woven into the framing so the agent knows
// what it's looking at - e.g. "Laravel 11 e-commerce app, multi-tenant".
func BuildAuditSummaryPrompt(schema *Schema, summary AuditSummary, sqlPath, projectContext string) string {
	var sb strings.Builder
	sb.WriteString("A database audit has already been performed by Hangar's server-side rule engine.\n")
	sb.WriteString("The findings have been written as a runnable SQL migration file.\n\n")

	sb.WriteString("DATABASE\n")
	sb.WriteString(fmt.Sprintf("  Type     : %s\n", schema.DBType))
	sb.WriteString(fmt.Sprintf("  Name     : %s\n", schema.DBName))
	sb.WriteString(fmt.Sprintf("  Tables   : %d\n", schema.TableCount))
	sb.WriteString(fmt.Sprintf("  Columns  : %d\n", schema.TotalColumns))
	sb.WriteString(fmt.Sprintf("  FKs      : %d\n", len(schema.ForeignKeys)))
	if projectContext != "" {
		sb.WriteString(fmt.Sprintf("  Context  : %s\n", projectContext))
	}
	sb.WriteString("\n")

	sb.WriteString("FINDINGS\n")
	sb.WriteString(fmt.Sprintf("  Total    : %d\n", summary.Total))
	sb.WriteString(fmt.Sprintf("  CRITICAL : %d\n", summary.Critical))
	sb.WriteString(fmt.Sprintf("  HIGH     : %d\n", summary.High))
	sb.WriteString(fmt.Sprintf("  MEDIUM   : %d\n", summary.Medium))
	sb.WriteString(fmt.Sprintf("  LOW      : %d\n", summary.Low))
	sb.WriteString(fmt.Sprintf("  INFO     : %d\n\n", summary.Info))

	sb.WriteString("AUDIT FILE\n")
	sb.WriteString("  " + sqlPath + "\n\n")

	sb.WriteString("YOUR JOB\n")
	sb.WriteString("  1. Read the file above. It is grouped by severity, with one comment block\n")
	sb.WriteString("     per finding describing what is wrong + a runnable DDL/DML fix.\n")
	sb.WriteString("  2. Present a Markdown summary to the user:\n")
	sb.WriteString("     - One line per CRITICAL finding (table.column + the issue).\n")
	sb.WriteString("     - HIGH grouped by category (Indexing, Security, Schema Integrity, ...).\n")
	sb.WriteString("     - MEDIUM and LOW collapsed to counts; offer to expand on request.\n")
	sb.WriteString("  3. Recommend the top 3-5 fixes by ROI and offer to apply them via the\n")
	sb.WriteString("     `run_sql` MCP tool. Do NOT apply anything without user confirmation.\n")
	sb.WriteString("  4. Anything starting with `-- decide:` or `-- migration:` in the file is\n")
	sb.WriteString("     a hint, not a runnable statement - flag those as needing a plan.\n\n")

	sb.WriteString("The rules engine is deterministic - re-running inspect_database after the\n")
	sb.WriteString("user applies fixes will produce a smaller findings set so progress is\n")
	sb.WriteString("measurable.\n")
	return sb.String()
}

// BuildAuditPrompt is kept as a back-compat alias that delegates to the
// new short summary prompt. The original 200-line audit prompt was
// retired when the rule engine moved into Audit(); callers that still
// pass a Schema get a short pointer at the SQL file instead.
//
// Deprecated: prefer BuildAuditSummaryPrompt with explicit sqlPath +
// AuditSummary so the agent gets useful counts.
func BuildAuditPrompt(schema *Schema, projectContext string) string {
	// Run the rule engine inline so legacy callers still get a useful
	// answer even if they haven't migrated to BuildAuditSummaryPrompt.
	// No SQL file is written here - that's the MCP tool's responsibility.
	// Empty dialect = auto-detect from schema.DBType.
	findings := Audit(schema, "")
	return BuildAuditSummaryPrompt(schema, summarize(findings), "(no SQL file - call inspect_database via MCP for that)", projectContext)
}

// legacyAuditPrompt is the original 200-line agent prompt we used before
// the rule engine moved into Audit(). Kept around (unused) only as
// reference for the rules we promised to cover.
func legacyAuditPrompt(schema *Schema, projectContext string) string {
	var sb strings.Builder
	sb.WriteString("You are a senior database architect, security auditor, and performance engineer\n")
	sb.WriteString("performing a deep, code-level audit of the database schema provided to you.\n\n")
	sb.WriteString("You will receive a JSON object describing a database (db_type, db_name, tables,\n")
	sb.WriteString("columns, indexes, foreign_keys, triggers, views, stored_procedures, row counts,\n")
	sb.WriteString("and any sample data provided). Treat this as the SINGLE SOURCE OF TRUTH. Do not\n")
	sb.WriteString("invent tables, columns, or relationships that are not in the input. If something\n")
	sb.WriteString("is ambiguous, say so explicitly under \"Assumptions\".\n\n")
	sb.WriteString("Your job is to identify EVERY meaningful issue - not just the obvious ones -\n")
	sb.WriteString("across the categories below, then return a structured report.\n\n")

	if projectContext != "" {
		sb.WriteString("================================================================================\n")
		sb.WriteString("PROJECT CONTEXT\n")
		sb.WriteString("================================================================================\n")
		sb.WriteString(projectContext)
		sb.WriteString("\n\n")
	}

	sb.WriteString(`================================================================================
GROUND RULES
================================================================================
1. Evidence-first. Every finding MUST cite the exact table.column or
   table.index/fk that triggered it. No vague statements like "some tables
   may need indexes".
2. Severity is mandatory and uses this scale:
   - CRITICAL : data loss, corruption, security breach, or production outage risk
   - HIGH     : real bugs, integrity holes, severe performance cliffs, or
                compliance violations
   - MEDIUM   : maintainability, scalability, or moderate performance issues
   - LOW      : style, naming, minor optimizations
   - INFO     : observations, not problems
3. Every finding MUST include a concrete fix as runnable DDL/DML for the target
   db_type (MySQL syntax if db_type = mysql, etc.). If the fix requires a
   migration plan (e.g., changing a PK on a 100M-row table), give the plan.
4. Do NOT pad. If a category has no issues, say "No issues found" for that
   category - don't fabricate findings to look thorough.
5. Be opinionated. You are the senior engineer in the room.

================================================================================
AUDIT CATEGORIES - CHECK EVERY ONE
================================================================================

## 1. Schema Integrity & Constraints
- Tables missing a primary key, or using a non-stable PK (e.g., email, name).
- Tables using composite PKs where a surrogate would simplify joins, and vice
  versa.
- Foreign-key columns with NO declared FK constraint. Detect by scanning every
  column matching ` + "`<noun>_id`, `<noun>Id`, `id_<noun>`, `<noun>_uuid`, or `<noun>_code`" + `
  and checking if a matching FK row exists. Report each orphan join column
  with the inferred parent table.
- FK constraints with wrong ON DELETE / ON UPDATE behavior given the business
  meaning (e.g., orders.customer_id ON DELETE CASCADE is almost always wrong;
  audit_log.user_id ON DELETE CASCADE is destructive).
- Self-referencing FKs without cycle protection.
- Columns that should be NOT NULL but are nullable (PKs, FKs, created_at,
  status, tenant_id, anything appearing in unique indexes).
- Columns that should be nullable but are NOT NULL with a meaningless default
  (e.g., '0000-00-00', '', 0 used as sentinel).
- Missing UNIQUE constraints on natural keys (email, username, slug, sku,
  external_id, stripe_customer_id, etc.).
- Missing CHECK constraints on enums-as-strings, ranges (age >= 0,
  amount >= 0), or status columns.
- Use of ENUM where a lookup table is more appropriate (changing an ENUM
  requires ALTER TABLE).
- Reserved words used as identifiers (` + "`order`, `user`, `group`, `key`" + `, etc.).

## 2. Data Type Correctness
For EVERY column, judge whether the type is correct given the column name and
context. Flag at minimum:
- Money / amount / price / cost / fee / balance stored as FLOAT, DOUBLE, or
  REAL. These MUST be DECIMAL(p,s). This is a CRITICAL finding.
- Dates/timestamps stored as VARCHAR, CHAR, or INT-epoch when DATETIME /
  TIMESTAMP would serve. Flag CRITICAL if the column drives business logic.
- created_at / updated_at not using DEFAULT CURRENT_TIMESTAMP and
  ON UPDATE CURRENT_TIMESTAMP (MySQL).
- IDs stored as VARCHAR when they are numeric, or as INT when they should be
  BIGINT (risk of auto_increment exhaustion above ~2.1B).
- UUIDs stored as VARCHAR(36) instead of BINARY(16) - call out the storage
  and index-bloat cost.
- Booleans stored as VARCHAR('Y'/'N', 'true'/'false') or INT without a CHECK
  constraint limiting to 0/1.
- TEXT / BLOB / JSON used where VARCHAR(n) would suffice, or vice versa.
- utf8 (3-byte) instead of utf8mb4 in MySQL - breaks emoji and some CJK.
- Mismatched collations between columns that join.
- Overly wide VARCHAR (e.g., VARCHAR(255) for a country code, VARCHAR(8000)
  for a name).
- Phone numbers stored as INT (loses leading zeros, +, formatting).
- IP addresses as VARCHAR instead of INET / VARBINARY(16).

## 3. Normalization & Modeling
- 1NF violations: comma-separated lists, JSON arrays used as multi-value
  columns where a child table is correct.
- 2NF/3NF violations: non-key columns depending on part of a composite key, or
  on another non-key column.
- Repeating column groups (phone1, phone2, phone3 / address_line_1 ..._5).
- Polymorphic associations without discriminator integrity (e.g.,
  commentable_type + commentable_id with no FK and no CHECK).
- EAV (entity-attribute-value) patterns that should be proper columns, OR
  proper columns that should be EAV / JSON given cardinality.
- Lookup tables that are missing (status, role, type stored as free-text
  strings repeated across rows).
- Missing junction tables for what is clearly a many-to-many.
- Denormalization without a documented reason (duplicated columns across
  tables that will drift).

## 4. Indexing & Query Performance
- FK columns without an index (in MySQL/InnoDB, FK columns get an auto-index,
  but only if no usable index already exists - verify, don't assume).
- Columns that obviously appear in WHERE / ORDER BY / GROUP BY based on their
  name (status, created_at, deleted_at, tenant_id, user_id, email) without an
  index.
- Composite indexes with wrong column order (low-cardinality column first
  when it should be last, or vice versa for range scans).
- Redundant indexes: idx(a) when idx(a,b) already exists.
- Duplicate indexes (same columns, different names).
- Over-indexing on small / write-heavy tables.
- Missing covering indexes for hot read paths inferable from column groupings.
- Indexes on low-cardinality boolean columns alone (rarely useful).
- Missing indexes on (tenant_id, <something>) in apparent multi-tenant
  schemas.
- Full-text search done via LIKE '%x%' patterns implied by columns named
  ` + "`description`, `body`, `content`" + ` - recommend FULLTEXT or external search.
- Soft-delete column (` + "`deleted_at`" + `) without a partial / filtered index or
  without being part of composite indexes - every query will scan deleted
  rows.

## 5. Security & Sensitive Data
This category gets EXTRA scrutiny. Be aggressive.
- Plain-text password / secret / token / api_key / private_key / access_token
  / refresh_token / session_token columns. CRITICAL.
- Password columns named ` + "`password`, `pwd`, `passwd`" + ` without obvious hashing
  indication (length too short for bcrypt/argon2 hash, type is VARCHAR(32) or
  shorter). CRITICAL.
- PII columns (ssn, social_security, tax_id, national_id, passport, dob,
  date_of_birth, full_name, address, phone, email) stored without any
  indication of encryption-at-rest or tokenization. HIGH minimum.
- PCI data (card_number, cvv, cvc, card_pan, expiry) - storing CVV is a hard
  PCI-DSS violation. CRITICAL.
- PHI columns (diagnosis, medical_record, prescription, etc.) under HIPAA.
- Audit / log tables that are mutable (no append-only design, FK CASCADE
  enabled, UPDATE/DELETE triggers).
- Triggers or stored procedures that build dynamic SQL via CONCAT of inputs
  (SQL injection via stored procs).
- Triggers that grant or escalate privileges, or write to security-sensitive
  tables without explicit authorization checks.
- Missing tenant_id on tables in an apparent multi-tenant schema (cross-tenant
  data leakage risk). CRITICAL.
- Views with SECURITY DEFINER (Postgres) or DEFINER= a privileged user
  (MySQL) that expose more than they should.
- Email/username uniqueness enforced case-sensitively when it shouldn't be.
- GDPR: no obvious mechanism for right-to-erasure (FKs with RESTRICT
  everywhere, no anonymization columns).

## 6. Concurrency, Transactions & Consistency
- Missing optimistic-locking column (` + "`version`, `lock_version`, `etag`" + `) on
  tables that look like aggregates / shared mutable state (account, inventory,
  wallet, balance).
- Counter columns (likes_count, views_count) updated via UPDATE ... SET
  count=count+1 implied - flag as a hot row / contention point and suggest
  alternatives.
- Trigger chains that can cause deadlocks or recursive trigger loops.
- Lack of unique constraints on idempotency keys, causing duplicate
  processing.
- Money movements without a double-entry ledger structure where the domain
  obviously needs one (transfers, payments, refunds).

## 7. Operational & Lifecycle
- Missing created_at / updated_at on tables that need them (basically all
  business tables).
- Missing soft-delete pattern where rows are obviously not deletable
  (audit_log, orders, invoices) - OR soft-delete columns that aren't
  consistently respected across the schema.
- Tables with no apparent archive / partitioning strategy that will obviously
  grow unbounded (events, logs, sessions, notifications, audit_trail).
- AUTO_INCREMENT on INT (not BIGINT) for tables expected to grow large.
- Tables using MyISAM (no transactions, no FK) - CRITICAL in MySQL.
- TIMESTAMP vs DATETIME confusion in MySQL (TIMESTAMP has 2038 problem and
  timezone conversion semantics).
- Missing ` + "`tenant_id` / `org_id`" + ` propagation across child tables.

## 8. Triggers, Views, Stored Procedures
For each trigger / view / proc provided:
- Hidden business logic that belongs in the application layer.
- Performance traps (row-by-row cursors, N+1 inside triggers).
- Recursive or cross-table cascades that aren't obvious from the schema.
- SECURITY DEFINER misuse.
- Missing error handling (no DECLARE ... HANDLER in MySQL procs).

## 9. Naming & Conventions
- Inconsistent pluralization (users vs order, customer vs products).
- Inconsistent casing (camelCase mixed with snake_case).
- Cryptic abbreviations (cust_dt_crt).
- Hungarian notation (tbl_, str_, int_).
- Columns named ` + "`data`, `info`, `value`, `extra`" + ` with no clear meaning.
- Same concept named differently across tables (customer_id vs client_id vs
  user_id for the same entity).

## 10. db_type-Specific Pitfalls
If db_type = mysql, additionally check:
- Tables not using InnoDB.
- Row format not DYNAMIC / COMPRESSED where appropriate.
- utf8 instead of utf8mb4.
- ONLY_FULL_GROUP_BY-incompatible patterns implied by views.
- ZEROFILL, DISPLAY WIDTH on INT (deprecated).
- BIT(1) for booleans (driver pain).
- Implicit type coercion risk on JOINs across mismatched types.

================================================================================
FINAL INSTRUCTIONS
================================================================================
- Do not stop early. Audit every table. A schema with 24 tables should
  typically produce 30-100 findings across all severities; if you produce
  fewer than 10 on a real schema, you are missing things.
- Never recommend changes that contradict each other.
- Never recommend ORM-specific advice - stay at the database level.
- If sample data is provided, USE IT to validate type choices, cardinality
  guesses, and length assumptions.
- Today's audit is for a production system. Write findings as if the on-call
  engineer will run your fix_sql tonight.
`)
	return sb.String()
}
