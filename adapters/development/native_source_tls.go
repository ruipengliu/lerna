package development

import (
	"crypto/tls"
	"os"
	"path/filepath"

	"github.com/ruipengliu/lerna/api"
)

// ForeignSourceTLSConfig 是受信 Source 入站 HTTPS 的固定文件引用；不包含凭据正文。
type ForeignSourceTLSConfig struct {
	CertificateFile string `json:"certificate_file"`
	KeyFile         string `json:"key_file"`
}

func validateForeignSourceTLS(c Config) error {
	files := c.ForeignSourceTLS
	if files == nil {
		return nil
	}
	if len(c.ForeignConsumers) == 0 && c.RemoteAgent == nil || !filepath.IsAbs(files.CertificateFile) || !filepath.IsAbs(files.KeyFile) {
		return api.E("forbidden", "explicit_foreign_source_tls_files_required")
	}
	if p := c.EndpointChannels; p != nil && (files.CertificateFile != p.GatewayTLS.CertificateFile || files.KeyFile != p.GatewayTLS.KeyFile) {
		return api.E("forbidden", "foreign_source_endpoint_tls_conflict")
	}
	return nil
}
func foreignSourceTLSFile(path string, bound int64, private bool) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > bound || private && info.Mode().Perm()&0077 != 0 {
		return nil, api.E("forbidden", "foreign_source_tls_file_invalid")
	}
	return os.ReadFile(path)
}
func (a *App) configureForeignSourceTLS() error {
	if err := validateForeignSourceTLS(a.Config); err != nil {
		return err
	}
	files := a.Config.ForeignSourceTLS
	if files == nil {
		return nil
	}
	// 已配置的 20 gateway/application TLS 仍由原装配负责；准确一致时只复用。
	// application 的内部 mTLS 不能被 Source 外部 HTTPS 替换。
	if a.endpointServerTLS != nil {
		return nil
	}
	cert, err := foreignSourceTLSFile(files.CertificateFile, 64<<10, false)
	if err != nil {
		return err
	}
	key, err := foreignSourceTLSFile(files.KeyFile, 16<<10, true)
	if err != nil {
		return err
	}
	pair, err := tls.X509KeyPair(cert, key)
	if err != nil {
		failure := api.E("forbidden", "foreign_source_tls_certificate_unavailable")
		failure.Cause = err
		return failure
	}
	a.endpointServerTLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}}
	return nil
}
