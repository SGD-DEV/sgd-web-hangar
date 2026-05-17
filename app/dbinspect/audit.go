package dbinspect

// audit.go is a server-side rule engine. It walks a Schema produced by
// Inspect() and produces a list of Findings - each finding ships with a
// runnable DDL/DML fix for the target db_type. The MCP tool layer then
// writes those fixes to a .sql file the agent can hand straight to the
// developer instead of asking the agent to re-derive everything from a
// raw schema dump.
//
// The rule set is intentionally exhaustive and lives in code (not in a
// prompt) for two reasons:
//
//   1. Determinism. The same schema in produces the same findings out, so
//      audits are reproducible and diffable across runs.
//   2. Speed. ~30 rules over a 200-table schema runs in milliseconds. We
//      don't have to wait for an LLM to score the schema.
//
// New rules should be additive and follow the same shape: detect, emit a
// Finding with severity + table + column + a one-line description + a
// runnable Fix (preferably idempotent).

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Severity ordering matches what users expect in a triage queue: CRITICAL
// first. WriteFixesSQL sorts by this scale so the top of the file is what
// you fix tonight, the bottom is what you fix this quarter.
const (
	SevCritical = "CRITICAL"
	SevHigh     = "HIGH"
	SevMedium   = "MEDIUM"
	SevLow      = "LOW"
	SevInfo     = "INFO"
)

var sevRank = map[string]int{
	SevCritical: 0,
	SevHigh:     1,
	SevMedium:   2,
	SevLow:      3,
	SevInfo:     4,
}

// Finding is one issue the rule engine surfaced. Table + Column point at
// the offending object; for cross-cutting findings (e.g. naming
// inconsistency across many tables) Table = "*" and Column = "".
type Finding struct {
	Severity    string `json:"severity"`
	Category    string `json:"category"`
	Table       string `json:"table"`
	Column      string `json:"column,omitempty"`
	Description string `json:"description"`
	FixSQL      string `json:"fix_sql"`
}

// AuditSummary is the count-by-severity returned to the MCP caller. Useful
// for the agent to decide whether to show the file in full or surface only
// the criticals.
type AuditSummary struct {
	Total    int `json:"total"`
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Info     int `json:"info"`
}

func summarize(findings []Finding) AuditSummary {
	s := AuditSummary{Total: len(findings)}
	for _, f := range findings {
		switch f.Severity {
		case SevCritical:
			s.Critical++
		case SevHigh:
			s.High++
		case SevMedium:
			s.Medium++
		case SevLow:
			s.Low++
		case SevInfo:
			s.Info++
		}
	}
	return s
}

// Tables/columns the rule engine should treat as framework noise rather
// than user data. Skipping them on multi-tenancy / created_at / lifecycle
// rules avoids a flood of false positives on Django/Rails plumbing.
var djangoFramework = map[string]bool{
	"auth_group":                              true,
	"auth_group_permissions":                  true,
	"auth_permission":                         true,
	"auth_user":                               true,
	"auth_user_groups":                        true,
	"auth_user_user_permissions":              true,
	"django_admin_log":                        true,
	"django_content_type":                     true,
	"django_migrations":                       true,
	"django_session":                          true,
	"token_blacklist_blacklistedtoken":        true,
	"token_blacklist_outstandingtoken":        true,
	"authtoken_token":                         true,
	"account_emailaddress":                    true,
	"account_emailconfirmation":               true,
	"socialaccount_socialaccount":             true,
	"socialaccount_socialapp":                 true,
	"socialaccount_socialtoken":               true,
	"socialaccount_socialapp_sites":           true,
	"django_site":                             true,
}

var tenantCols = []string{
	"tenant_id", "organization_id", "org_id", "company_id",
	"shop_id", "store_id", "workspace_id", "account_id",
}

var moneyHints = []string{
	"amount", "price", "cost", "fee", "balance", "total",
	"subtotal", "discount", "tax", "paid", "due",
	"revenue", "expense", "salary", "wage", "rate",
}

var naturalKeyHints = []string{
	"email", "username", "slug", "sku", "external_id",
	"stripe_customer_id", "api_key", "invoice_number", "order_number", "barcode",
}

var plainTextHints = []string{
	"password", "pwd", "passwd", "secret", "api_key", "private_key",
	"token", "access_token", "refresh_token", "session_token", "webhook_secret",
}

var piiHints = []string{
	"ssn", "social_security", "tax_id", "national_id",
	"passport", "dob", "date_of_birth", "cnic",
}

var pciHints = []string{"card_number", "cvv", "cvc", "card_pan", "pan"}

var pgReserved = map[string]bool{
	"order": true, "user": true, "group": true, "check": true,
	"desc": true, "asc": true, "limit": true, "offset": true,
	"select": true, "where": true, "when": true, "case": true,
	"table": true, "column": true, "index": true,
}

var unboundedHints = []string{
	"event", "log", "session", "notification",
	"audit", "activity", "history", "message", "webhook",
}

var nounIDPattern = regexp.MustCompile(`^(\w+)_(id|uuid|code)$`)
var typeLengthPattern = regexp.MustCompile(`\((\d+)\)`)

// Audit runs the full rule set over schema and returns the findings in
// declared rule order. Caller can sort by severity via SortFindings.
func Audit(schema *Schema) []Finding {
	if schema == nil {
		return nil
	}

	// Pre-build lookup tables once; rules below read them many times.
	fkSet := make(map[string]bool, len(schema.ForeignKeys))
	fksByTable := make(map[string][]ForeignKey, len(schema.Tables))
	for _, fk := range schema.ForeignKeys {
		fkSet[fk.Table+"."+fk.Column] = true
		fksByTable[fk.Table] = append(fksByTable[fk.Table], fk)
	}

	indexedCols := make(map[string]map[string]bool, len(schema.Tables))
	uniqueSingleCols := make(map[string]bool)
	tableNames := make(map[string]bool, len(schema.Tables))
	colsInTable := make(map[string]map[string]bool, len(schema.Tables))

	for _, t := range schema.Tables {
		tableNames[t.Name] = true
		set := make(map[string]bool, len(t.Indexes)*2)
		for _, ix := range t.Indexes {
			for _, c := range ix.Columns {
				set[c] = true
			}
			if ix.Unique && len(ix.Columns) == 1 {
				uniqueSingleCols[t.Name+"."+ix.Columns[0]] = true
			}
		}
		indexedCols[t.Name] = set

		cols := make(map[string]bool, len(t.Columns))
		for _, c := range t.Columns {
			cols[c.Name] = true
		}
		colsInTable[t.Name] = cols
	}

	var findings []Finding
	add := func(sev, category, table, col, desc, fix string) {
		findings = append(findings, Finding{
			Severity:    sev,
			Category:    category,
			Table:       table,
			Column:      col,
			Description: desc,
			FixSQL:      fix,
		})
	}

	// ---- 1. Schema Integrity ----

	// 1a. Tables without PK
	for _, t := range schema.Tables {
		if len(t.PrimaryKey) == 0 {
			add(SevHigh, "Schema Integrity", t.Name, "",
				"No primary key declared",
				fmt.Sprintf("ALTER TABLE %s ADD COLUMN id BIGSERIAL PRIMARY KEY;", t.Name))
		}
	}

	// 1b. Orphan FK columns - columns named like a foreign key with no FK
	// constraint behind them. Try to suggest the parent table by
	// matching the noun against existing table names.
	for _, t := range schema.Tables {
		for _, c := range t.Columns {
			m := nounIDPattern.FindStringSubmatch(c.Name)
			if m == nil {
				continue
			}
			if c.Name == "id" {
				continue
			}
			if fkSet[t.Name+"."+c.Name] {
				continue
			}
			noun := m[1]
			suggested := "<table for \"" + noun + "\">"
			for tn := range tableNames {
				if tn == noun || tn == noun+"s" || strings.HasSuffix(tn, "_"+noun) {
					suggested = tn
					break
				}
			}
			add(SevHigh, "Schema Integrity", t.Name, c.Name,
				fmt.Sprintf("Orphan join column - looks like FK to \"%s\" but no FOREIGN KEY constraint", suggested),
				fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT fk_%s_%s FOREIGN KEY (%s) REFERENCES %s(id) ON DELETE RESTRICT;",
					t.Name, t.Name, c.Name, c.Name, suggested))
		}
	}

	// 1c. FKs without explicit ON DELETE
	for _, fk := range schema.ForeignKeys {
		od := strings.ToUpper(fk.OnDelete)
		if od != "" && od != "NO ACTION" {
			continue
		}
		isAudit := strings.Contains(fk.Table, "audit") || strings.Contains(fk.Table, "log") || strings.Contains(fk.Table, "history")
		sev := SevMedium
		recommend := "RESTRICT or CASCADE"
		if isAudit {
			sev = SevHigh
			recommend = "SET NULL"
		}
		add(sev, "Schema Integrity", fk.Table, fk.Column,
			fmt.Sprintf("FK to %s has no explicit ON DELETE (defaults to NO ACTION/RESTRICT)", fk.ReferencedTbl),
			fmt.Sprintf("-- decide: %s based on business meaning, then ALTER CONSTRAINT", recommend))
	}

	// 1d. CASCADE on audit/log/history tables - destroys the audit trail
	for _, fk := range schema.ForeignKeys {
		if strings.ToUpper(fk.OnDelete) != "CASCADE" {
			continue
		}
		if strings.Contains(fk.Table, "audit") || strings.Contains(fk.Table, "log") || strings.Contains(fk.Table, "history") {
			add(SevCritical, "Schema Integrity", fk.Table, fk.Column,
				fmt.Sprintf("ON DELETE CASCADE on audit/log/history table - deletes audit trail when %s row is removed", fk.ReferencedTbl),
				fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT <fk_name>; ALTER TABLE %s ADD CONSTRAINT ... ON DELETE SET NULL;",
					fk.Table, fk.Table))
		}
	}

	// 1e. Nullable columns that should be NOT NULL
	neverNull := map[string]bool{
		"tenant_id": true, "organization_id": true, "org_id": true,
		"company_id": true, "shop_id": true, "store_id": true,
		"created_at": true, "status": true,
	}
	for _, t := range schema.Tables {
		for _, c := range t.Columns {
			if neverNull[c.Name] && c.Nullable {
				add(SevHigh, "Schema Integrity", t.Name, c.Name,
					fmt.Sprintf("Column '%s' is nullable but semantically should be NOT NULL", c.Name),
					fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET NOT NULL;  -- backfill first if needed",
						t.Name, c.Name))
			}
		}
	}

	// 1f. Missing UNIQUE on natural keys
	for _, t := range schema.Tables {
		for _, c := range t.Columns {
			matched := false
			for _, h := range naturalKeyHints {
				if c.Name == h || strings.HasSuffix(c.Name, "_"+h) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			if uniqueSingleCols[t.Name+"."+c.Name] {
				continue
			}
			if c.IsPrimaryKey {
				continue
			}
			add(SevHigh, "Schema Integrity", t.Name, c.Name,
				fmt.Sprintf("Natural-key column '%s' has no UNIQUE constraint - duplicates possible", c.Name),
				fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s_%s_uniq UNIQUE (%s);",
					t.Name, t.Name, c.Name, c.Name))
		}
	}

	// Reserved words used as identifiers
	for _, t := range schema.Tables {
		if pgReserved[strings.ToLower(t.Name)] {
			add(SevLow, "Naming", t.Name, "",
				fmt.Sprintf("Table name '%s' is a PostgreSQL reserved word", t.Name),
				"-- always quote when referencing")
		}
		for _, c := range t.Columns {
			if pgReserved[strings.ToLower(c.Name)] {
				add(SevLow, "Naming", t.Name, c.Name,
					fmt.Sprintf("Column name '%s' is a reserved word", c.Name),
					"-- always quote when referencing")
			}
		}
	}

	// ---- 2. Data Type Correctness ----
	timeSuffix := []string{"_at", "_on"}
	timeFull := map[string]bool{"date": true, "time": true, "timestamp": true}
	for _, t := range schema.Tables {
		for _, c := range t.Columns {
			ctype := strings.ToLower(c.Type)
			cn := strings.ToLower(c.Name)

			// Money as float
			matchesMoney := false
			for _, h := range moneyHints {
				if h == cn || strings.Contains(cn, "_"+h) || strings.HasSuffix(cn, "_"+h) {
					matchesMoney = true
					break
				}
			}
			if matchesMoney {
				switch ctype {
				case "real", "double precision", "float", "float4", "float8":
					add(SevCritical, "Data Type", t.Name, c.Name,
						fmt.Sprintf("Money-like column stored as %s - precision loss for currency", c.Type),
						fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE NUMERIC(14,2) USING %s::numeric(14,2);",
							t.Name, c.Name, c.Name))
				}
			}

			// Time-looking column stored as text/int
			isTimeName := timeFull[cn]
			if !isTimeName {
				for _, sfx := range timeSuffix {
					if strings.HasSuffix(cn, sfx) {
						isTimeName = true
						break
					}
				}
			}
			if isTimeName {
				if strings.HasPrefix(ctype, "character") || ctype == "text" || ctype == "integer" || ctype == "bigint" {
					add(SevCritical, "Data Type", t.Name, c.Name,
						fmt.Sprintf("Time-looking column '%s' stored as %s - lose timezone awareness, can't index by date range", c.Name, c.Type),
						"-- migration: add new TIMESTAMPTZ column, parse and copy, drop old, rename")
				}
			}

			// Timestamp w/o tz on common time columns
			switch cn {
			case "created_at", "updated_at", "last_login", "expires_at", "deleted_at":
				if ctype == "timestamp without time zone" {
					add(SevHigh, "Data Type", t.Name, c.Name,
						fmt.Sprintf("'%s' uses TIMESTAMP WITHOUT TIME ZONE - in a multi-tenant SaaS use TIMESTAMPTZ to avoid silent UTC/local drift", c.Name),
						fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE TIMESTAMPTZ USING %s AT TIME ZONE 'UTC';",
							t.Name, c.Name, c.Name))
				}
			}

			// INT id on hot tables
			if ctype == "integer" && (c.IsPrimaryKey || strings.HasSuffix(cn, "_id")) {
				for _, h := range []string{"event", "log", "transaction", "order", "sale", "invoice", "session", "audit", "line"} {
					if strings.Contains(t.Name, h) {
						add(SevHigh, "Data Type", t.Name, c.Name,
							"INTEGER (max ~2.1B) on a table that grows quickly",
							fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE BIGINT;  -- locks table briefly",
								t.Name, c.Name))
						break
					}
				}
			}

			// Phone as INT
			switch cn {
			case "phone", "phone_number", "mobile", "mobile_number", "contact_number", "contact_no":
				if ctype == "integer" || ctype == "bigint" {
					add(SevHigh, "Data Type", t.Name, c.Name,
						fmt.Sprintf("Phone number stored as %s - loses leading zeros, +, formatting", c.Type),
						fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE VARCHAR(20) USING %s::text;",
							t.Name, c.Name, c.Name))
				}
			}

			// Boolean-looking column stored as char/varchar
			if strings.HasPrefix(ctype, "character") {
				if strings.HasPrefix(cn, "is_") || strings.HasPrefix(cn, "has_") ||
					strings.HasPrefix(cn, "can_") || strings.HasPrefix(cn, "should_") {
					add(SevMedium, "Data Type", t.Name, c.Name,
						fmt.Sprintf("Boolean-looking column '%s' stored as %s", c.Name, c.Type),
						fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE BOOLEAN USING (%s IN ('true','t','1','y','yes'));",
							t.Name, c.Name, c.Name))
				}
			}

			// Code columns oversized
			switch cn {
			case "country", "country_code", "currency", "currency_code", "language_code", "locale":
				if m := typeLengthPattern.FindStringSubmatch(c.Type); m != nil {
					var n int
					fmt.Sscanf(m[1], "%d", &n)
					if n > 16 {
						add(SevLow, "Data Type", t.Name, c.Name,
							fmt.Sprintf("Code column '%s' is %s - oversized for ISO codes", c.Name, c.Type),
							fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE VARCHAR(8);", t.Name, c.Name))
					}
				}
			}
		}
	}

	// ---- 5. Security & Sensitive Data (run before indexing so the file
	// surfaces secrets above performance issues at the same severity).

	for _, t := range schema.Tables {
		for _, c := range t.Columns {
			cn := strings.ToLower(c.Name)
			ctype := strings.ToLower(c.Type)

			// Plain-text secrets / tokens
			matched := ""
			for _, h := range plainTextHints {
				if h == cn || strings.Contains(cn, "_"+h) || strings.HasSuffix(cn, h) {
					matched = h
					break
				}
			}
			if matched != "" {
				// auth_user.password is always Django's hashed PBKDF2; whitelist.
				if !(t.Name == "auth_user" && cn == "password") {
					short := false
					if m := typeLengthPattern.FindStringSubmatch(c.Type); m != nil {
						var n int
						fmt.Sscanf(m[1], "%d", &n)
						if n < 60 {
							short = true
						}
					}
					desc := fmt.Sprintf("Sensitive column '%s' (%s) - confirm hashed/encrypted at rest", c.Name, c.Type)
					if short {
						desc += " (length<60: too short for bcrypt/argon2)"
					}
					add(SevCritical, "Security", t.Name, c.Name, desc,
						"-- if plaintext: hash via app layer; for reversible secrets use pgcrypto column-level encryption")
				}
			}

			// PCI - cardholder data
			pciHit := ""
			for _, h := range pciHints {
				if h == cn || containsToken(cn, h) {
					pciHit = h
					break
				}
			}
			if pciHit != "" {
				if pciHit == "cvv" || pciHit == "cvc" {
					add(SevCritical, "Security", t.Name, c.Name,
						"Storing CVV/CVC is a hard PCI-DSS violation regardless of encryption",
						fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;  -- never store CVV; tokenize via Stripe/Adyen",
							t.Name, c.Name))
				} else {
					add(SevCritical, "Security", t.Name, c.Name,
						fmt.Sprintf("Card data column '%s' - storing PAN requires PCI-DSS Level 1 + HSM", c.Name),
						fmt.Sprintf("-- replace with payment_method_token reference; drop %s", c.Name))
				}
			}

			// PII
			for _, h := range piiHints {
				if h == cn || containsToken(cn, h) {
					add(SevHigh, "Security", t.Name, c.Name,
						fmt.Sprintf("PII column '%s' - confirm encryption-at-rest or tokenization (GDPR/CCPA)", c.Name),
						"-- pgcrypto: ALTER TABLE ... ALTER COLUMN ... TYPE BYTEA USING pgp_sym_encrypt(...)")
					break
				}
			}
			_ = ctype // reserved for future type-aware security rules
		}
	}

	// Multi-tenancy: missing tenant_id on a table that smells like user
	// data (has created_at OR has an FK to users).
	for _, t := range schema.Tables {
		if djangoFramework[t.Name] {
			continue
		}
		if strings.HasSuffix(t.Name, "_permissions") || strings.Contains(t.Name, "_groups") {
			continue
		}
		cols := colsInTable[t.Name]
		hasTenant := false
		for _, tc := range tenantCols {
			if cols[tc] {
				hasTenant = true
				break
			}
		}
		if hasTenant {
			continue
		}
		hasUserFK := false
		for _, fk := range fksByTable[t.Name] {
			if cols[fk.Column] && strings.Contains(fk.ReferencedTbl, "user") {
				hasUserFK = true
				break
			}
		}
		if cols["created_at"] || hasUserFK {
			add(SevCritical, "Security", t.Name, "",
				"No tenant_id / organization_id / shop_id - cross-tenant data leakage risk in multi-tenant SaaS",
				fmt.Sprintf("ALTER TABLE %s ADD COLUMN tenant_id BIGINT NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT; CREATE INDEX ON %s(tenant_id);",
					t.Name, t.Name))
		}
	}

	// ---- 4. Indexing & Query Performance ----

	// FK columns without an index
	for _, fk := range schema.ForeignKeys {
		if !indexedCols[fk.Table][fk.Column] {
			add(SevHigh, "Indexing", fk.Table, fk.Column,
				fmt.Sprintf("FK column '%s' has no index - JOINs to %s full-scan", fk.Column, fk.ReferencedTbl),
				fmt.Sprintf("CREATE INDEX ON %s (%s);", fk.Table, fk.Column))
		}
	}

	// Soft-delete column without partial index
	for _, t := range schema.Tables {
		if colsInTable[t.Name]["deleted_at"] && !indexedCols[t.Name]["deleted_at"] {
			add(SevMedium, "Indexing", t.Name, "deleted_at",
				"Soft-delete 'deleted_at' has no partial index - every query scans deleted rows",
				fmt.Sprintf("CREATE INDEX %s_active_idx ON %s (id) WHERE deleted_at IS NULL;",
					t.Name, t.Name))
		}
	}

	// Likely-filtered columns without an index
	for _, t := range schema.Tables {
		for _, c := range t.Columns {
			cn := strings.ToLower(c.Name)
			isFilter := cn == "status" || cn == "state" ||
				strings.HasSuffix(cn, "_status") || strings.HasSuffix(cn, "_state")
			if !isFilter {
				continue
			}
			if strings.ToLower(c.Type) == "boolean" {
				continue
			}
			if indexedCols[t.Name][c.Name] {
				continue
			}
			add(SevMedium, "Indexing", t.Name, c.Name,
				fmt.Sprintf("Likely-filtered column '%s' has no index", c.Name),
				fmt.Sprintf("CREATE INDEX ON %s (%s);", t.Name, c.Name))
		}
	}

	// Multi-tenant composite index missing
	for _, t := range schema.Tables {
		cols := colsInTable[t.Name]
		var tcol string
		for _, tc := range tenantCols {
			if cols[tc] {
				tcol = tc
				break
			}
		}
		if tcol == "" || !cols["created_at"] {
			continue
		}
		hasComposite := false
		for _, ix := range t.Indexes {
			if len(ix.Columns) > 1 && ix.Columns[0] == tcol {
				hasComposite = true
				break
			}
		}
		if !hasComposite {
			add(SevMedium, "Indexing", t.Name, tcol,
				fmt.Sprintf("Missing composite index on (%s, created_at) - hot read path in multi-tenant", tcol),
				fmt.Sprintf("CREATE INDEX ON %s (%s, created_at DESC);", t.Name, tcol))
		}
	}

	// ---- 7. Operational & Lifecycle ----

	for _, t := range schema.Tables {
		if djangoFramework[t.Name] {
			continue
		}
		if strings.HasSuffix(t.Name, "_permissions") || strings.Contains(t.Name, "_groups") {
			continue
		}
		cols := colsInTable[t.Name]
		if !cols["created_at"] && !cols["created"] && !cols["date_created"] {
			add(SevLow, "Operational", t.Name, "",
				"No created_at column - timestamps essential for debugging, sorting, sync",
				fmt.Sprintf("ALTER TABLE %s ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();", t.Name))
		}
	}

	for _, t := range schema.Tables {
		matchesUnbounded := false
		for _, u := range unboundedHints {
			if strings.Contains(t.Name, u) {
				matchesUnbounded = true
				break
			}
		}
		if !matchesUnbounded {
			continue
		}
		cols := colsInTable[t.Name]
		if cols["created_at"] || cols["timestamp"] || cols["occurred_at"] {
			add(SevLow, "Operational", t.Name, "",
				"Likely unbounded - plan archive/partitioning (PG declarative partitioning by created_at month)",
				"-- once row count > ~10M, convert to partitioned table by RANGE (created_at)")
		}
	}

	// ---- 9. Naming ----

	// Inconsistent user-FK column naming across the schema
	userLike := map[string]int{}
	for _, fk := range schema.ForeignKeys {
		switch fk.ReferencedTbl {
		case "auth_user", "users", "user":
			userLike[fk.Column]++
		}
	}
	if len(userLike) > 1 {
		// Sort for deterministic output
		type kv struct {
			k string
			v int
		}
		var sorted []kv
		for k, v := range userLike {
			sorted = append(sorted, kv{k, v})
		}
		sort.Slice(sorted, func(i, j int) bool {
			if sorted[i].v != sorted[j].v {
				return sorted[i].v > sorted[j].v
			}
			return sorted[i].k < sorted[j].k
		})
		var parts []string
		for _, p := range sorted {
			parts = append(parts, fmt.Sprintf("%s(%d)", p.k, p.v))
		}
		add(SevLow, "Naming", "*", "",
			"User reference uses inconsistent column names: "+strings.Join(parts, ", "),
			"-- pick one (typically user_id) and rename others")
	}

	// Generic dumping-ground columns
	generic := map[string]bool{
		"data": true, "info": true, "value": true,
		"extra": true, "meta": true, "metadata": true, "misc": true,
	}
	for _, t := range schema.Tables {
		for _, c := range t.Columns {
			if !generic[strings.ToLower(c.Name)] {
				continue
			}
			ctype := strings.ToLower(c.Type)
			if ctype == "text" || ctype == "jsonb" || ctype == "json" {
				add(SevLow, "Naming", t.Name, c.Name,
					fmt.Sprintf("Generic column '%s' (%s) - risk of becoming undocumented dumping ground", c.Name, c.Type),
					"-- if structured: split into typed columns; if variable: document JSON schema in column comment")
			}
		}
	}

	// ---- 8. Triggers / sprocs ----
	for _, tr := range schema.Triggers {
		body := strings.ToUpper(tr.Statement)
		if strings.Contains(body, "EXECUTE") && strings.Contains(body, "||") && !strings.Contains(body, "EXECUTE FORMAT") {
			add(SevHigh, "Triggers", tr.Table, "",
				fmt.Sprintf("Trigger '%s' builds dynamic SQL via concatenation - SQL injection risk", tr.Name),
				"-- replace || with format(..., %L, %I) parameterization")
		}
	}
	for _, p := range schema.StoredProcedures {
		if strings.EqualFold(p.Security, "DEFINER") {
			add(SevMedium, "Triggers", "*", p.Name,
				fmt.Sprintf("Stored procedure '%s' uses SECURITY DEFINER - verify can't be tricked into escalation", p.Name),
				"-- audit; SET search_path = pg_catalog, pg_temp; consider INVOKER if escalation isn't intentional")
		}
	}

	return findings
}

// containsToken returns true if needle appears as a snake_case token of
// hay (so "card_pan" matches "pan", but "japan" doesn't).
func containsToken(hay, needle string) bool {
	for _, tok := range strings.Split(hay, "_") {
		if tok == needle {
			return true
		}
	}
	return false
}

// SortFindings orders findings by severity descending then by table for
// stable diffs. Not strictly required by WriteFixesSQL (which sorts
// internally per-section) but useful for callers that want to render the
// list in a UI.
func SortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		ri := sevRank[findings[i].Severity]
		rj := sevRank[findings[j].Severity]
		if ri != rj {
			return ri < rj
		}
		if findings[i].Table != findings[j].Table {
			return findings[i].Table < findings[j].Table
		}
		return findings[i].Column < findings[j].Column
	})
}

// WriteFixesSQL emits a self-contained .sql file with one block per
// finding, grouped by severity. Returns the absolute path so the MCP tool
// can hand it back to the agent. The file format is plain-text SQL with
// `--` comments above each fix so an engineer can read it top-to-bottom
// or `psql -f` it after review.
//
// File location: <outputDir>/<dbName>-audit-<timestamp>.sql. outputDir
// is created if missing.
func WriteFixesSQL(schema *Schema, findings []Finding, outputDir string) (string, error) {
	if schema == nil {
		return "", fmt.Errorf("dbinspect: nil schema")
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", fmt.Errorf("dbinspect: creating audit dir: %w", err)
	}

	stamp := time.Now().Format("20060102-150405")
	dbName := schema.DBName
	if dbName == "" {
		dbName = "db"
	}
	filename := fmt.Sprintf("%s-audit-%s.sql", sanitizeFilename(dbName), stamp)
	path := filepath.Join(outputDir, filename)

	summary := summarize(findings)

	// Bucket by severity in declared order so CRITICAL is at the top of
	// the file.
	bySev := map[string][]Finding{}
	for _, f := range findings {
		bySev[f.Severity] = append(bySev[f.Severity], f)
	}
	for sev := range bySev {
		fs := bySev[sev]
		sort.SliceStable(fs, func(i, j int) bool {
			if fs[i].Category != fs[j].Category {
				return fs[i].Category < fs[j].Category
			}
			if fs[i].Table != fs[j].Table {
				return fs[i].Table < fs[j].Table
			}
			return fs[i].Column < fs[j].Column
		})
		bySev[sev] = fs
	}

	var sb strings.Builder
	sb.WriteString("-- =====================================================================\n")
	sb.WriteString(fmt.Sprintf("-- Hangar database audit - %s (%s)\n", schema.DBName, schema.DBType))
	sb.WriteString(fmt.Sprintf("-- Generated: %s\n", time.Now().Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("-- Tables: %d   Columns: %d   Foreign keys: %d   Findings: %d\n",
		schema.TableCount, schema.TotalColumns, len(schema.ForeignKeys), summary.Total))
	sb.WriteString(fmt.Sprintf("-- Severity counts: CRITICAL=%d  HIGH=%d  MEDIUM=%d  LOW=%d  INFO=%d\n",
		summary.Critical, summary.High, summary.Medium, summary.Low, summary.Info))
	sb.WriteString("--\n")
	sb.WriteString("-- HOW TO USE THIS FILE\n")
	sb.WriteString("--   1. Read top-to-bottom. CRITICAL is first; LOW is last.\n")
	sb.WriteString("--   2. Each finding is one block: a comment header explaining what's\n")
	sb.WriteString("--      wrong, then a runnable DDL/DML statement.\n")
	sb.WriteString("--   3. Statements are NOT wrapped in a transaction - apply them one\n")
	sb.WriteString("--      at a time after reviewing each. Some require backfilling data\n")
	sb.WriteString("--      first; the comment will say so.\n")
	sb.WriteString("--   4. Anything starting with `-- decide:` or `-- migration:` is a\n")
	sb.WriteString("--      hint, not a runnable statement - turn it into a real plan\n")
	sb.WriteString("--      before applying.\n")
	sb.WriteString("-- =====================================================================\n\n")

	severityOrder := []string{SevCritical, SevHigh, SevMedium, SevLow, SevInfo}
	for _, sev := range severityOrder {
		fs := bySev[sev]
		if len(fs) == 0 {
			continue
		}
		sb.WriteString("-- =====================================================================\n")
		sb.WriteString(fmt.Sprintf("-- %s  (%d finding%s)\n", sev, len(fs), pluralS(len(fs))))
		sb.WriteString("-- =====================================================================\n\n")

		for i, f := range fs {
			loc := f.Table
			if f.Column != "" {
				loc = f.Table + "." + f.Column
			}
			sb.WriteString(fmt.Sprintf("-- [%s] #%d  %s  (%s)\n", f.Severity, i+1, loc, f.Category))
			// Wrap description across line breaks with `--` prefix
			for _, line := range strings.Split(f.Description, "\n") {
				sb.WriteString("--   " + line + "\n")
			}
			fix := strings.TrimSpace(f.FixSQL)
			if fix == "" {
				fix = "-- (no automatic fix - investigate manually)"
			}
			sb.WriteString(fix)
			if !strings.HasSuffix(fix, "\n") {
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}
	}

	if summary.Total == 0 {
		sb.WriteString("-- No issues found by the rule engine. Schema looks clean at this layer;\n")
		sb.WriteString("-- this does NOT replace a domain-aware human review.\n")
	}

	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return "", fmt.Errorf("dbinspect: writing audit file: %w", err)
	}
	return path, nil
}

// AuditAndWrite is the convenience entry point used by the MCP tool: run
// the rules, write the SQL file, return everything the caller needs in
// one call.
func AuditAndWrite(schema *Schema, outputDir string) (path string, findings []Finding, summary AuditSummary, err error) {
	findings = Audit(schema)
	SortFindings(findings)
	summary = summarize(findings)
	path, err = WriteFixesSQL(schema, findings, outputDir)
	return path, findings, summary, err
}

func sanitizeFilename(name string) string {
	var sb strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-' || r == '_':
			sb.WriteRune(r)
		default:
			sb.WriteRune('_')
		}
	}
	if sb.Len() == 0 {
		return "db"
	}
	return sb.String()
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
