// Package dns offers the DNS-over-TLS provider catalog and a live probe.
// Writing to Keenetic is done through keenetic.Adapter.ApplyDoT.
package dns

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/BROadmin/broray/internal/keenetic"
)

// Catalog lists the providers BROray can manage (3.x routes/dot catalog).
var Catalog = []keenetic.DoTServer{
	{ID: "cloudflare-primary", Provider: "Cloudflare", Address: "1.1.1.1", Port: 853, SNI: "cloudflare-dns.com"},
	{ID: "cloudflare-secondary", Provider: "Cloudflare", Address: "1.0.0.1", Port: 853, SNI: "cloudflare-dns.com"},
	{ID: "google-primary", Provider: "Google", Address: "8.8.8.8", Port: 853, SNI: "dns.google"},
	{ID: "google-secondary", Provider: "Google", Address: "8.8.4.4", Port: 853, SNI: "dns.google"},
	{ID: "quad9-primary", Provider: "Quad9", Address: "9.9.9.9", Port: 853, SNI: "dns.quad9.net"},
	{ID: "quad9-secondary", Provider: "Quad9", Address: "149.112.112.112", Port: 853, SNI: "dns.quad9.net"},
	{ID: "adguard-primary", Provider: "AdGuard", Address: "94.140.14.14", Port: 853, SNI: "dns.adguard-dns.com"},
	{ID: "adguard-secondary", Provider: "AdGuard", Address: "94.140.15.15", Port: 853, SNI: "dns.adguard-dns.com"},
	{ID: "yandex-primary", Provider: "Yandex", Address: "77.88.8.8", Port: 853, SNI: "common.dot.dns.yandex.net"},
	{ID: "yandex-secondary", Provider: "Yandex", Address: "77.88.8.1", Port: 853, SNI: "common.dot.dns.yandex.net"},
}

func Find(id string) (keenetic.DoTServer, bool) {
	for _, s := range Catalog {
		if s.ID == id {
			return s, true
		}
	}
	return keenetic.DoTServer{}, false
}

// Probe opens a TLS session with SNI verification and resolves "example.com"
// A record over it (RFC 7858). It returns the round-trip time.
func Probe(ctx context.Context, s keenetic.DoTServer) (time.Duration, error) {
	d := &net.Dialer{Timeout: 5 * time.Second}
	start := time.Now()
	raw, err := d.DialContext(ctx, "tcp", net.JoinHostPort(s.Address, strconv.Itoa(s.Port)))
	if err != nil {
		return 0, fmt.Errorf("DOT_CONNECT_FAILED: %v", err)
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(8 * time.Second))
	conn := tls.Client(raw, &tls.Config{ServerName: s.SNI, MinVersion: tls.VersionTLS12})
	if err := conn.HandshakeContext(ctx); err != nil {
		return 0, fmt.Errorf("DOT_TLS_FAILED: %v", err)
	}
	query := buildQuery("example.com")
	msg := make([]byte, 2+len(query))
	binary.BigEndian.PutUint16(msg, uint16(len(query)))
	copy(msg[2:], query)
	if _, err := conn.Write(msg); err != nil {
		return 0, fmt.Errorf("DOT_WRITE_FAILED: %v", err)
	}
	var hdr [2]byte
	if _, err := readFull(conn, hdr[:]); err != nil {
		return 0, fmt.Errorf("DOT_READ_FAILED: %v", err)
	}
	n := int(binary.BigEndian.Uint16(hdr[:]))
	if n < 12 || n > 4096 {
		return 0, errors.New("DOT_RESPONSE_INVALID")
	}
	resp := make([]byte, n)
	if _, err := readFull(conn, resp); err != nil {
		return 0, fmt.Errorf("DOT_READ_FAILED: %v", err)
	}
	if resp[0] != query[0] || resp[1] != query[1] || resp[2]&0x80 == 0 {
		return 0, errors.New("DOT_RESPONSE_MISMATCH")
	}
	if rcode := resp[3] & 0x0f; rcode != 0 {
		return 0, fmt.Errorf("DOT_RCODE_%d", rcode)
	}
	return time.Since(start), nil
}

func readFull(c net.Conn, b []byte) (int, error) {
	got := 0
	for got < len(b) {
		n, err := c.Read(b[got:])
		got += n
		if err != nil {
			return got, err
		}
	}
	return got, nil
}

func buildQuery(name string) []byte {
	b := []byte{0x42, 0x24, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0}
	for _, label := range splitLabels(name) {
		b = append(b, byte(len(label)))
		b = append(b, label...)
	}
	b = append(b, 0, 0, 1, 0, 1) // QTYPE A, QCLASS IN
	return b
}

func splitLabels(name string) []string {
	var out []string
	cur := ""
	for _, r := range name {
		if r == '.' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
