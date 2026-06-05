package mqtt

import "testing"

func TestNewTLSConfigDisabledByDefault(t *testing.T) {
	cfg, err := NewTLSConfig(TLSFiles{})
	if err != nil {
		t.Fatalf("new tls config: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config when TLS is not configured")
	}
}

func TestNewTLSConfigAllowsExplicitInsecureSkipVerify(t *testing.T) {
	cfg, err := NewTLSConfig(TLSFiles{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("new tls config: %v", err)
	}
	if cfg == nil || !cfg.InsecureSkipVerify {
		t.Fatalf("expected insecure TLS config, got %+v", cfg)
	}
}

func TestNewTLSConfigRejectsMissingCAFile(t *testing.T) {
	if _, err := NewTLSConfig(TLSFiles{CAFile: "/path/does/not/exist"}); err == nil {
		t.Fatalf("expected missing CA file error")
	}
}

func TestNewTLSConfigRequiresCertAndKeyPair(t *testing.T) {
	if _, err := NewTLSConfig(TLSFiles{CertFile: "client.crt"}); err == nil {
		t.Fatalf("expected missing key error")
	}
	if _, err := NewTLSConfig(TLSFiles{KeyFile: "client.key"}); err == nil {
		t.Fatalf("expected missing cert error")
	}
}
