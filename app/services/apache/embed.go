package apache

import _ "embed"

//go:embed templates/httpd.conf.tmpl
var httpdConfTmpl string

//go:embed templates/vhost.conf.tmpl
var vhostConfTmpl string
