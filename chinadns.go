package chinadns

import (
	"context"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/forward"
	"github.com/coredns/coredns/plugin/pkg/nonwriter"
	"github.com/miekg/dns"
	"github.com/oschwald/maxminddb-golang"
	"net"
)

type dnsReply struct {
	reply *dns.Msg
	code  int
	err   error
}

type ChinaDNS struct {
	cnFwd *forward.Forward
	fbFwd *forward.Forward
	geoIP *maxminddb.Reader
	Next  plugin.Handler
}

func New() *ChinaDNS {
	return &ChinaDNS{
		cnFwd: forward.New(),
		fbFwd: forward.New(),
		Next:  nil,
	}
}

func (c *ChinaDNS) ServeDNS(ctx context.Context, writer dns.ResponseWriter, msg *dns.Msg) (int, error) {
	cnReply := make(chan *dnsReply, 1)
	fbReply := make(chan *dnsReply, 1)
	msgCopy := *msg
	go func() {
		resp := nonwriter.New(writer)
		code, err := c.cnFwd.ServeDNS(ctx, resp, msg)
		cnReply <- &dnsReply{resp.Msg, code, err}
	}()
	go func() {
		resp := nonwriter.New(writer)
		code, err := c.fbFwd.ServeDNS(ctx, resp, &msgCopy)
		fbReply <- &dnsReply{resp.Msg, code, err}
	}()
	// First of all, we must wait for a reply of china dns.
	cnMsg := <-cnReply
	if cnMsg.reply != nil && len(cnMsg.reply.Answer) > 0 && c.isChina(cnMsg.reply) {
		writer.WriteMsg(cnMsg.reply)
		return cnMsg.code, cnMsg.err
	}
	fbMsg := <-fbReply
	if fbMsg.reply != nil {
		writer.WriteMsg(fbMsg.reply)
	}
	return fbMsg.code, fbMsg.err
}

func (c *ChinaDNS) Name() string { return "chinadns" }

func (c *ChinaDNS) isChina(r *dns.Msg) bool {
	for _, ans := range r.Answer {
		var ip net.IP
		switch ans.Header().Rrtype {
		case dns.TypeA:
			ip = ans.(*dns.A).A
		case dns.TypeAAAA:
			ip = ans.(*dns.AAAA).AAAA
		default:
			continue
		}
		var record struct {
			Country struct {
				ISOCode string `maxminddb:"iso_code"`
			} `maxminddb:"country"`
		}
		err := c.geoIP.Lookup(ip, &record)
		if err != nil {
			return false
		}
		if record.Country.ISOCode == "CN" {
			return true
		}
	}
	return false
}
