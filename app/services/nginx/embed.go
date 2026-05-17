package nginx

import _ "embed"

//go:embed templates/nginx.conf.tmpl
var nginxConfTmpl string

//go:embed templates/site.conf.tmpl
var siteConfTmpl string
