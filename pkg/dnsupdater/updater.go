package dnsupdater

import (
	"fmt"
	"net"

	mdns "github.com/miekg/dns"
)

type TSIGConfig struct {
	Key    string
	Secret string
}

type Updater struct {
	server string
	zone   string
	ttl    uint32
	tsig   *TSIGConfig
	owner  string
}

func New(server, zone string, ttl uint32, tsig *TSIGConfig, owner string) *Updater {
	return &Updater{server: server, zone: zone, ttl: ttl, tsig: tsig, owner: owner}
}

// Register inserts an A record for hostname and a TXT ownership record at
// dockerdns-{hostname} in a single DNS UPDATE message.
func (u *Updater) Register(hostname, ip, resource string) error {
	msg := new(mdns.Msg)
	msg.SetUpdate(mdns.Fqdn(u.zone))

	fqdn := mdns.Fqdn(hostname)
	a := &mdns.A{
		Hdr: mdns.RR_Header{
			Name:   fqdn,
			Rrtype: mdns.TypeA,
			Class:  mdns.ClassINET,
			Ttl:    u.ttl,
		},
		A: net.ParseIP(ip).To4(),
	}

	txtFqdn := mdns.Fqdn("dockerdns-" + hostname)
	txt := &mdns.TXT{
		Hdr: mdns.RR_Header{
			Name:   txtFqdn,
			Rrtype: mdns.TypeTXT,
			Class:  mdns.ClassINET,
			Ttl:    u.ttl,
		},
		Txt: []string{
			fmt.Sprintf("heritage=dockerdns,dockerdns/owner=%s,dockerdns/resource=%s", u.owner, resource),
		},
	}

	msg.Insert([]mdns.RR{a, txt})
	return u.send(msg)
}

// Deregister removes all records at hostname and at dockerdns-{hostname}.
func (u *Updater) Deregister(hostname, ip string) error {
	msg := new(mdns.Msg)
	msg.SetUpdate(mdns.Fqdn(u.zone))

	fqdn := mdns.Fqdn(hostname)
	txtFqdn := mdns.Fqdn("dockerdns-" + hostname)

	msg.RemoveName([]mdns.RR{
		&mdns.ANY{Hdr: mdns.RR_Header{Name: fqdn, Rrtype: mdns.TypeANY, Class: mdns.ClassANY}},
		&mdns.ANY{Hdr: mdns.RR_Header{Name: txtFqdn, Rrtype: mdns.TypeANY, Class: mdns.ClassANY}},
	})
	return u.send(msg)
}

func (u *Updater) send(msg *mdns.Msg) error {
	if u.tsig != nil {
		msg.SetTsig(u.tsig.Key, mdns.HmacSHA256, 300, 0)
	}

	c := new(mdns.Client)
	if u.tsig != nil {
		c.TsigSecret = map[string]string{u.tsig.Key: u.tsig.Secret}
	}

	resp, _, err := c.Exchange(msg, u.server)
	if err != nil {
		return fmt.Errorf("dns exchange: %w", err)
	}
	if resp.Rcode != mdns.RcodeSuccess {
		return fmt.Errorf("dns update failed: %s", mdns.RcodeToString[resp.Rcode])
	}
	return nil
}
