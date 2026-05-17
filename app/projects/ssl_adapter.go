package projects

import (
	"github.com/devour-app/devour/app/ssl"
)

// sslAdapter wraps ssl.Manager to implement SSLCertGenerator interface
type sslAdapter struct {
	mgr *ssl.Manager
}

// NewSSLAdapter creates an SSLCertGenerator from an ssl.Manager
func NewSSLAdapter(mgr *ssl.Manager) SSLCertGenerator {
	return &sslAdapter{mgr: mgr}
}

func (a *sslAdapter) GenerateCert(domain string) error {
	return a.mgr.GenerateCert(domain)
}

func (a *sslAdapter) GetCert(domain string) (SSLCertInfo, error) {
	info, err := a.mgr.GetCert(domain)
	if err != nil {
		return SSLCertInfo{}, err
	}
	return SSLCertInfo{
		CertPath: info.CertPath,
		KeyPath:  info.KeyPath,
	}, nil
}
