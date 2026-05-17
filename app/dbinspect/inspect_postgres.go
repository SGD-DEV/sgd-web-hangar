package dbinspect

import (
	"context"
	"database/sql"
	"strings"
)

// inspectPostgres uses pg_catalog (more accurate than information_schema for
// pgsql-specific things like generated columns, attislocal, etc.) but falls
// back to information_schema for cross-engine consistency where it doesn't
// matter.
func inspectPostgres(ctx context.Context, db *sql.DB, dbName string, schema *Schema) error {
	if dbName == "" {
		row := db.QueryRowContext(ctx, "SELECT current_database()")
		_ = row.Scan(&schema.DBName)
		dbName = schema.DBName
	}

	// --- Tables + columns ---
	colSQL := `
SELECT
  c.table_name, c.column_name, c.data_type, c.is_nullable, c.column_default,
  COALESCE(pg_catalog.col_description(format('%I.%I', c.table_schema, c.table_name)::regclass::oid, c.ordinal_position), '') AS comment,
  CASE WHEN tc.constraint_type = 'PRIMARY KEY' THEN 1 ELSE 0 END AS is_pk
FROM information_schema.columns c
LEFT JOIN information_schema.key_column_usage kcu
  ON kcu.table_schema = c.table_schema AND kcu.table_name = c.table_name AND kcu.column_name = c.column_name
LEFT JOIN information_schema.table_constraints tc
  ON tc.constraint_name = kcu.constraint_name AND tc.constraint_type = 'PRIMARY KEY'
WHERE c.table_schema NOT IN ('pg_catalog', 'information_schema')
ORDER BY c.table_name, c.ordinal_position`
	rows, err := db.QueryContext(ctx, colSQL)
	if err != nil {
		return err
	}
	defer rows.Close()
	tableMap := map[string]*TableInfo{}
	tableOrder := []string{}
	for rows.Next() {
		var (
			table, colName, dataType, nullable, comment string
			def                                         sql.NullString
			isPK                                        int
		)
		if err := rows.Scan(&table, &colName, &dataType, &nullable, &def, &comment, &isPK); err != nil {
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
			Type:         dataType,
			Nullable:     strings.EqualFold(nullable, "YES"),
			IsPrimaryKey: isPK == 1,
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
SELECT
  t.relname AS table_name,
  i.relname AS index_name,
  ix.indisunique AS is_unique,
  array_agg(a.attname ORDER BY array_position(ix.indkey::int[], a.attnum)) AS cols
FROM pg_class t
JOIN pg_index ix ON ix.indrelid = t.oid
JOIN pg_class i ON i.oid = ix.indexrelid
JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY(ix.indkey)
JOIN pg_namespace n ON n.oid = t.relnamespace
WHERE n.nspname NOT IN ('pg_catalog', 'information_schema') AND t.relkind = 'r'
GROUP BY t.relname, i.relname, ix.indisunique
ORDER BY t.relname, i.relname`
	if rows2, err := db.QueryContext(ctx, idxSQL); err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var (
				tableName, idxName string
				isUnique           bool
				colsRaw            string
			)
			if err := rows2.Scan(&tableName, &idxName, &isUnique, &colsRaw); err != nil {
				return err
			}
			// pq returns array as "{a,b,c}"
			colsRaw = strings.Trim(colsRaw, "{}")
			cols := strings.Split(colsRaw, ",")
			ix := IndexInfo{Name: idxName, Columns: cols, Unique: isUnique}
			if t, ok := tableMap[tableName]; ok {
				t.Indexes = append(t.Indexes, ix)
			}
		}
	}

	// --- Foreign keys ---
	fkSQL := `
SELECT
  kcu.table_name, kcu.column_name,
  ccu.table_name AS referenced_table, ccu.column_name AS referenced_column,
  COALESCE(rc.delete_rule, ''), COALESCE(rc.update_rule, '')
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
JOIN information_schema.constraint_column_usage ccu
  ON ccu.constraint_name = tc.constraint_name AND ccu.table_schema = tc.table_schema
LEFT JOIN information_schema.referential_constraints rc
  ON rc.constraint_name = tc.constraint_name
WHERE tc.constraint_type = 'FOREIGN KEY'
  AND tc.table_schema NOT IN ('pg_catalog', 'information_schema')
ORDER BY kcu.table_name, kcu.column_name`
	if rows3, err := db.QueryContext(ctx, fkSQL); err == nil {
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
SELECT trigger_name, event_object_table, event_manipulation, action_timing, action_statement
FROM information_schema.triggers
WHERE trigger_schema NOT IN ('pg_catalog', 'information_schema')`
	if rows4, err := db.QueryContext(ctx, trgSQL); err == nil {
		defer rows4.Close()
		for rows4.Next() {
			var t TriggerInfo
			if err := rows4.Scan(&t.Name, &t.Table, &t.Event, &t.Timing, &t.Statement); err != nil {
				return err
			}
			schema.Triggers = append(schema.Triggers, t)
		}
	}

	// --- Approximate row counts ---
	rcSQL := `
SELECT relname, COALESCE(reltuples::bigint, 0)
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind = 'r' AND n.nspname NOT IN ('pg_catalog', 'information_schema')`
	if rows5, err := db.QueryContext(ctx, rcSQL); err == nil {
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
SELECT
  table_name,
  view_definition,
  COALESCE(check_option, '') AS sec
FROM information_schema.views
WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
ORDER BY table_name`
	if rows6, err := db.QueryContext(ctx, viewSQL); err == nil {
		defer rows6.Close()
		for rows6.Next() {
			var v ViewInfo
			if err := rows6.Scan(&v.Name, &v.Definition, &v.Security); err != nil {
				return err
			}
			schema.Views = append(schema.Views, v)
		}
	}

	// --- Stored procs + functions (Postgres) ---
	// pg_proc joined with pg_language to get the language; pg_get_functiondef
	// returns the full CREATE FUNCTION/PROCEDURE statement which is what the
	// audit prompt actually wants for SECURITY DEFINER analysis.
	procSQL := `
SELECT
  p.proname,
  CASE p.prokind WHEN 'f' THEN 'FUNCTION' WHEN 'p' THEN 'PROCEDURE' WHEN 'a' THEN 'AGGREGATE' WHEN 'w' THEN 'WINDOW' ELSE 'UNKNOWN' END AS kind,
  l.lanname,
  pg_get_functiondef(p.oid) AS definition,
  CASE p.prosecdef WHEN true THEN 'DEFINER' ELSE 'INVOKER' END AS security,
  pg_get_userbyid(p.proowner) AS definer,
  pg_get_function_arguments(p.oid) AS parameters
FROM pg_proc p
JOIN pg_namespace n ON n.oid = p.pronamespace
JOIN pg_language l ON l.oid = p.prolang
WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY p.proname`
	if rows7, err := db.QueryContext(ctx, procSQL); err == nil {
		defer rows7.Close()
		for rows7.Next() {
			var p StoredProcedure
			if err := rows7.Scan(&p.Name, &p.Type, &p.Language, &p.Definition, &p.Security, &p.Definer, &p.Parameters); err != nil {
				return err
			}
			schema.StoredProcedures = append(schema.StoredProcedures, p)
		}
	}

	for _, name := range tableOrder {
		schema.Tables = append(schema.Tables, *tableMap[name])
	}
	return nil
}
