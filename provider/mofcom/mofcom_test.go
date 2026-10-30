package mofcom

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFDIHTTPClientTLSCompatibility(t *testing.T) {
	var negotiated uint16
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		negotiated = r.TLS.CipherSuite
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success"}`))
	}))
	server.TLS = &tls.Config{
		MinVersion: tls.VersionTLS12,
		MaxVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
		},
	}
	server.StartTLS()
	defer server.Close()

	transport := newFDIHTTPClient().Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	transport.TLSClientConfig.RootCAs = server.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	client := &http.Client{Transport: transport}
	defer transport.CloseIdleConnections()

	resp, err := client.Get(server.URL)
	require.NoError(t, err, "TLS 1.2 RSA-GCM 站点应可完成握手")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, uint16(tls.VersionTLS12), resp.TLS.Version)
	require.Contains(t, []uint16{tls.TLS_RSA_WITH_AES_128_GCM_SHA256, tls.TLS_RSA_WITH_AES_256_GCM_SHA384}, negotiated)
}

func TestFDIHTTPClientUsesOnlyTLS12RSACipherSuites(t *testing.T) {
	transport := newFDIHTTPClient().Transport.(*http.Transport)
	require.Equal(t, uint16(tls.VersionTLS12), transport.TLSClientConfig.MinVersion)
	require.Equal(t, uint16(tls.VersionTLS12), transport.TLSClientConfig.MaxVersion)
	require.Equal(t, []uint16{tls.TLS_RSA_WITH_AES_128_GCM_SHA256, tls.TLS_RSA_WITH_AES_256_GCM_SHA384}, transport.TLSClientConfig.CipherSuites)
}
