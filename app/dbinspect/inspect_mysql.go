package dbinspect

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// inspectMySQL fills schema.Tables / ForeignKeys / Triggers from MySQL/MariaDB
// information_schema. Single connection, multiple queries - we don't need to
// be clever, dev DBs are small.
func inspectMySQL(ctx context.Context, db *sql.DB, dbName string, schema *Schema) error {
	if dbName == "" {
		// SELECT DATABASE() returns NULL when no default db chosen.
		row := db.QueryRowContext(ctx, "SELECT DATABASE()")
		var d sql.NullString
		_ = row.Scan(&d)
		if !d.Valid || d.String == "" {
			return fmt.Errorf("no database selected; pass db_name or USE <db>")
		}
		schema.DBName = d.String
		dbName = d.String
	}

	// --- Tables + columns ---
	colSQL := `
SELECT
  c.TABLE_NAME, c.COLUMN_NAME, c.COLUMN_TYPE, c.IS_NULLABLE,
  c.COLUMN_DEFAULT, COALESCE(c.COLUMN_KEY, ''), COALESCE(c.COLUMN_COMMENT, '')
FROM information_schema.COLUMNS c
WHERE c.TABLE_SCHEMA = ?
ORDER BY c.TABLE_NAME, c.ORDINAL_POSITION`
	rows, err := db.QueryContext(ctx, colSQL, dbName)
	if err != nil {
		return fmt.Errorf("listing columns: %w", err)
	}
	defer rows.Close()
	tableMap := map[string]*TableInfo{} // preserves insertion order via separate slice
	tableOrder := []string{}
	for rows.Next() {
		var (
			table, colName, colType, nullable, key, comment string
			def                                             sql.NullString
		)
		if err := rows.Scan(&table, &colName, &colType, &nullable, &def, &key, &comment); err != nil {
			return err
		}
		t, ok := tableMap[table]
		if !ok {
			t = &TableInfo{Name: table}
			tableMap[table] = t
			tableOrder = append(tableOrder, table)
		}
		col := ColumnInfo{
			Name:         colName,
			Type:         colType,
			Nullable:     strings.EqualFold(nullable, "YES"),
			IsPrimaryKey: key == "PRI",
			Comment:      comment,
		}
		if def.Valid {
			s := def.String
			col.Default = &s
		}
		if col.IsPrimaryKey {
			t.PrimaryKey = append(t.PrimaryKey, colName)
		}
		t.Columns = append(t.Columns, col)
	}

	// --- Indexes ---
	idxSQL := `
SELECT TABLE_NAME, INDEX_NAME, NON_UNIQUE, COLUMN_NAME, SEQ_IN_INDEX
FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = ?
ORDER BY TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX`
	if rows2, err := db.QueryContext(ctx, idxSQL, dbName); err == nil {
		defer rows2.Close()
		// Group columns by (table, index_name) preserving order.
		type key struct{ table, name string }
		acc := map[key]*IndexInfo{}
		for rows2.Next() {
			var (
				table, idxName, col string
				nonUnique           int
				seq                 int
			)
			if err := rows2.Scan(&table, &idxName, &nonUnique, &col, &seq); err != nil {
				return err
			}
			k := key{table, idxName}
			ix, ok := acc[k]
			if !ok {
				ix = &IndexInfo{Name: idxName, Unique: nonUnique == 0}
				acc[k] = ix
				if t, ok := tableMap[table]; ok {
					t.Indexes = append(t.Indexes, IndexInfo{}) // placeholder, fill at end
				}
			}
			ix.Columns = append(ix.Columns, col)
		}
		// Replace the placeholder slices we appended above with the accumulated values.
		for _, t := range tableMap {
			t.Indexes = t.Indexes[:0]
		}
		for k, ix := range acc {
			if t, ok := tableMap[k.table]; ok {
				t.Indexes = append(t.Indexes, *ix)
			}
		}
	}

	// --- Foreign keys ---
	fkSQL := `
SELECT
  k.TABLE_NAME, k.COLUMN_NAME, k.REFERENCED_TABLE_NAME, k.REFERENCED_COLUMN_NAME,
  COALESCE(rc.DELETE_RULE, ''), COALESCE(rc.UPDATE_RULE, '')
FROM information_schema.KEY_COLUMN_USAGE k
LEFT JOIN information_schema.REFERENTIAL_CONSTRAINTS rc
  ON rc.CONSTRAINT_SCHEMA = k.CONSTRAINT_SCHEMA
 AND rc.CONSTRAINT_NAME   = k.CONSTRAINT_NAME
WHERE k.TABLE_SCHEMA = ? AND k.REFERENCED_TABLE_NAME IS NOT NULL
ORDER BY k.TABLE_NAME, k.COLUMN_NAME`
	if rows3, err := db.QueryContext(ctx, fkSQL, dbName); err == nil {
		defer rows3.Close()
		for rows3.Next() {
			var fk ForeignKey
			if err := rows3.Scan(&fk.Table, &fk.Column, &fk.ReferencedTbl, &fk.ReferencedCol, &fk.OnDelete, &fk.OnUpdate); err != nil {
				return err
			}
			schema.ForeignKeys = append(schema.ForeignKeys, fk)
		}
	}

	// --- Triggers ---
	trgSQL := `
SELECT TRIGGER_NAME, EVENT_OBJECT_TABLE, EVENT_MANIPULATION, ACTION_TIMING, ACTION_STATEMENT
FROM information_schema.TRIGGERS
WHERE TRIGGER_SCHEMA = ?`
	if rows4, err := db.QueryContext(ctx, trgSQL, dbName); err == nil {
		defer rows4.Close()
		for rows4.Next() {
			var t TriggerInfo
			if err := rows4.Scan(&t.Name, &t.Table, &t.Event, &t.Timing, &t.Statement); err != nil {
				return err
			}
			schema.Triggers = append(schema.Triggers, t)
		}
	}

	// --- Approximate row counts (cheap; uses TABLE_ROWS estimate) ---
	rcSQL := `
SELECT TABLE_NAME, COALESCE(TABLE_ROWS, 0)
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE'`
	if rows5, err := db.QueryContext(ctx, rcSQL, dbName); err == nil {
		defer rows5.Close()
		for rows5.Next() {
			var name string
			var rc int64
			_ = rows5.Scan(&name, &rc)
			if t, ok := tableMap[name]; ok {
				t.RowCount = rc
			}
		}
	}

	// --- Views ---
	viewSQL := `
SELECT TABLE_NAME, VIEW_DEFINITION, SECURITY_TYPE, COALESCE(DEFINER, '')
FROM information_schema.VIEWS
WHERE TABLE_SCHEMA = ?`
	if rows6, err := db.QueryContext(ctx, viewSQL, dbName); err == nil {
		defer rows6.Close()
		for rows6.Next() {
			var v ViewInfo
			if err := rows6.Scan(&v.Name, &v.Definition, &v.Security, &v.Definer); err != nil {
				return err
			}
			schema.Views = append(schema.Views, v)
		}
	}

	// --- Stored procedures + functions ---
	// ROUTINE_DEFINITION can be NULL for procs created with sql_mode that
	// hides the body; coalesce to empty so the JSON stays clean.
	procSQL := `
SELECT
  ROUTINE_NAME, ROUTINE_TYPE,
  COALESCE(ROUTINE_BODY, ''),
  COALESCE(ROUTINE_DEFINITION, ''),
  COALESCE(SECURITY_TYPE, ''),
  COALESCE(DEFINER, ''),
  COALESCE(DTD_IDENTIFIER, '')
FROM information_schema.ROUTINES
WHERE ROUTINE_SCHEMA = ?`
	if rows7, err := db.QueryContext(ctx, procSQL, dbName); err == nil {
		defer rows7.Close()
		for rows7.Next() {
			var p StoredProcedure
			if err := rows7.Scan(&p.Name, &p.Type, &p.Language, &p.Definition, &p.Security, &p.Definer, &p.Parameters); err != nil {
				return err
			}
			schema.StoredProcedures = append(schema.StoredProcedures, p)
		}
	}

	// Materialize tables in declared order.
	for _, name := range tableOrder {
		schema.Tables = append(schema.Tables, *tableMap[name])
	}
	return nil
}
