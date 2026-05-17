package postgresql

import _ "embed"

//go:embed templates/postgresql.conf.tmpl
var postgresqlConfTmpl string
