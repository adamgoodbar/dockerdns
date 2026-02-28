package dnsupdater_test

import (
	"net"
	"strings"
	"testing"
	"time"

	mdns "github.com/miekg/dns"

	"dockerdns/pkg/dnsupdater"
)

// startTestServer starts a raw UDP server on a random port. It captures the
// first DNS message it receives into the returned channel and replies with the
// given rcode.
func startTestServer(t *testing.T, rcode int) (addr string, received <-chan *mdns.Msg) {
	t.Helper()

	ch := make(chan *mdns.Msg, 1)

	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	t.Cleanup(func() { conn.Close() })

	go func() {
		buf := make([]byte, 4096)
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}

			msg := new(mdns.Msg)
			if err := msg.Unpack(buf[:n]); err != nil {
				continue
			}

			select {
			case ch <- msg.Copy():
			default:
			}

			resp := new(mdns.Msg)
			resp.Id = msg.Id
			resp.Response = true
			resp.Opcode = msg.Opcode
			resp.Rcode = rcode

			b, err := resp.Pack()
			if err != nil {
				continue
			}
			conn.WriteTo(b, from)
		}
	}()

	return conn.LocalAddr().String(), ch
}

func recv(t *testing.T, ch <-chan *mdns.Msg) *mdns.Msg {
	t.Helper()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for DNS message")
		return nil
	}
}

func TestRegister(t *testing.T) {
	addr, ch := startTestServer(t, mdns.RcodeSuccess)

	u := dnsupdater.New(addr, "example.com.", 60, nil, "default")
	if err := u.Register("app.example.com", "10.0.0.1", "container/abc123def456"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	msg := recv(t, ch)

	if msg.Opcode != mdns.OpcodeUpdate {
		t.Fatalf("expected UPDATE opcode, got %d", msg.Opcode)
	}
	t.Logf("opcode: %d (UPDATE)", msg.Opcode)

	if len(msg.Ns) != 2 {
		t.Fatalf("expected 2 update RRs (A + TXT), got %d", len(msg.Ns))
	}

	a, ok := msg.Ns[0].(*mdns.A)
	if !ok {
		t.Fatalf("expected A record first in update section, got %T", msg.Ns[0])
	}
	if a.Hdr.Name != "app.example.com." {
		t.Errorf("A name: expected app.example.com., got %s", a.Hdr.Name)
	}
	if !a.A.Equal(net.ParseIP("10.0.0.1")) {
		t.Errorf("A IP: expected 10.0.0.1, got %s", a.A)
	}
	if a.Hdr.Ttl != 60 {
		t.Errorf("A TTL: expected 60, got %d", a.Hdr.Ttl)
	}
	t.Logf("A record: name=%s ip=%s ttl=%d", a.Hdr.Name, a.A, a.Hdr.Ttl)

	txt, ok := msg.Ns[1].(*mdns.TXT)
	if !ok {
		t.Fatalf("expected TXT record second in update section, got %T", msg.Ns[1])
	}
	if txt.Hdr.Name != "dockerdns-app.example.com." {
		t.Errorf("TXT name: expected dockerdns-app.example.com., got %s", txt.Hdr.Name)
	}
	if len(txt.Txt) != 1 {
		t.Fatalf("expected 1 TXT string, got %d", len(txt.Txt))
	}
	t.Logf("TXT record: name=%s value=%q", txt.Hdr.Name, txt.Txt[0])
}

func TestRegister_TXTContent(t *testing.T) {
	addr, ch := startTestServer(t, mdns.RcodeSuccess)

	u := dnsupdater.New(addr, "example.com.", 60, nil, "myinstance")
	if err := u.Register("app.example.com", "10.0.0.1", "container/abc123def456"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	msg := recv(t, ch)
	if len(msg.Ns) < 2 {
		t.Fatalf("expected at least 2 RRs, got %d", len(msg.Ns))
	}

	txt, ok := msg.Ns[1].(*mdns.TXT)
	if !ok {
		t.Fatalf("expected TXT record, got %T", msg.Ns[1])
	}

	val := txt.Txt[0]
	if !strings.Contains(val, "heritage=dockerdns") {
		t.Errorf("TXT missing heritage=dockerdns: %q", val)
	}
	if !strings.Contains(val, "dockerdns/owner=myinstance") {
		t.Errorf("TXT missing dockerdns/owner=myinstance: %q", val)
	}
	if !strings.Contains(val, "dockerdns/resource=container/abc123def456") {
		t.Errorf("TXT missing dockerdns/resource=container/abc123def456: %q", val)
	}
	t.Logf("TXT value: %q", val)
}

func TestDeregister(t *testing.T) {
	addr, ch := startTestServer(t, mdns.RcodeSuccess)

	u := dnsupdater.New(addr, "example.com.", 60, nil, "default")
	if err := u.Deregister("app.example.com", "10.0.0.1"); err != nil {
		t.Fatalf("Deregister: %v", err)
	}

	msg := recv(t, ch)

	if msg.Opcode != mdns.OpcodeUpdate {
		t.Fatalf("expected UPDATE opcode, got %d", msg.Opcode)
	}
	t.Logf("opcode: %d (UPDATE)", msg.Opcode)

	if len(msg.Ns) != 2 {
		t.Fatalf("expected 2 remove RRs (A name + TXT name), got %d", len(msg.Ns))
	}

	hdr := msg.Ns[0].Header()
	if hdr.Name != "app.example.com." {
		t.Errorf("remove[0] name: expected app.example.com., got %s", hdr.Name)
	}
	if hdr.Class != mdns.ClassANY {
		t.Errorf("remove[0] class: expected ClassANY, got %d", hdr.Class)
	}
	t.Logf("remove A: name=%s class=%d (ANY)", hdr.Name, hdr.Class)

	txtHdr := msg.Ns[1].Header()
	if txtHdr.Name != "dockerdns-app.example.com." {
		t.Errorf("remove[1] name: expected dockerdns-app.example.com., got %s", txtHdr.Name)
	}
	if txtHdr.Class != mdns.ClassANY {
		t.Errorf("remove[1] class: expected ClassANY, got %d", txtHdr.Class)
	}
	t.Logf("remove TXT: name=%s class=%d (ANY)", txtHdr.Name, txtHdr.Class)
}

func TestRegister_ServerError(t *testing.T) {
	addr, _ := startTestServer(t, mdns.RcodeRefused)

	u := dnsupdater.New(addr, "example.com.", 60, nil, "default")
	err := u.Register("app.example.com", "10.0.0.1", "container/abc123def456")
	if err == nil {
		t.Fatal("expected error on REFUSED response")
	}
	t.Logf("got expected error: %v", err)
}

func TestRegister_WithTSIG(t *testing.T) {
	addr, ch := startTestServer(t, mdns.RcodeSuccess)

	tsig := &dnsupdater.TSIGConfig{
		Key:    "testkey.",
		Secret: "aGVsbG8=", // base64("hello")
	}
	u := dnsupdater.New(addr, "example.com.", 30, tsig, "default")
	// The test server doesn't sign its response, so the client will get a TSIG
	// verification error — that's expected. We only care that the outgoing
	// message carried a TSIG record, which we verify on the server side.
	u.Register("app.example.com", "10.0.0.2", "container/abc123def456") //nolint:errcheck

	msg := recv(t, ch)
	tsigRR := msg.IsTsig()
	if tsigRR == nil {
		t.Fatal("expected TSIG record on outgoing message")
	}
	t.Logf("TSIG present: key=%s algorithm=%s", tsigRR.Hdr.Name, tsigRR.Algorithm)
}
