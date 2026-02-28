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
}

func New(server, zone string, ttl uint32, tsig *TSIGConfig) *Updater {
	return &Updater{server: server, zone: zone, ttl: ttl, tsig: tsig}
}

func (u *Updater) Register(hostname, ip string) error {
	return u.update(hostname, ip, false)
}

func (u *Updater) Deregister(hostname, ip string) error {
	return u.update(hostname, ip, true)
}

func (u *Updater) update(hostname, ip string, remove bool) error {
	msg := new(mdns.Msg)
	msg.SetUpdate(mdns.Fqdn(u.zone))

	fqdn := mdns.Fqdn(hostname)

	if remove {
		rr := &mdns.RR_Header{
			Name:   fqdn,
			Rrtype: mdns.TypeANY,
			Class:  mdns.ClassANY,
			Ttl:    0,
		}
		msg.RemoveName([]mdns.RR{&mdns.ANY{Hdr: *rr}})
	} else {
		a := &mdns.A{
			Hdr: mdns.RR_Header{
				Name:   fqdn,
				Rrtype: mdns.TypeA,
				Class:  mdns.ClassINET,
				Ttl:    u.ttl,
			},
			A: net.ParseIP(ip).To4(),
		}
		msg.Insert([]mdns.RR{a})
	}

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
