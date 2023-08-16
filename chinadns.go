package chinadns

import (
	"context"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/forward"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/plugin/pkg/nonwriter"
	"github.com/coredns/coredns/request"

	"github.com/miekg/dns"
	"github.com/oschwald/maxminddb-golang"
)

var log = clog.NewWithPlugin("chinadns")

type dnsReply struct {
	reply *dns.Msg
	code  int
	err   error
}

type options struct {
	// The path of GeoIP database.
	path string
	// A list of domains to bypass fallback upstreams.
	ignored []string
	// The time between two reload of the db file.
	reload time.Duration
	// Prevent from passing a specific query type to next plugin.
	block uint16
}

func newOptions() *options {
	return &options{
		reload: 30 * time.Second,
	}
}

type ChinaDNS struct {
	sync.RWMutex
	fwd   *forward.Forward
	geoIP *maxminddb.Reader
	// mtime and size are only read and modified by a single goroutine.
	mtime time.Time
	size  int64
	opts  *options
	Next  plugin.Handler
}

func New() *ChinaDNS {
	return &ChinaDNS{
		fwd:  forward.New(),
		opts: newOptions(),
		Next: nil,
	}
}

func (c *ChinaDNS) ServeDNS(ctx context.Context, writer dns.ResponseWriter, msg *dns.Msg) (int, error) {
	state := request.Request{W: writer, Req: msg}
	if c.bypass(state.Name()) {
		return c.fwd.ServeDNS(ctx, writer, msg)
	}
	cnReply := make(chan *dnsReply, 1)
	fbReply := make(chan *dnsReply, 1)
	msgCopy := *msg
	// Forward to main upstreams.
	go func() {
		resp := nonwriter.New(writer)
		code, err := c.fwd.ServeDNS(ctx, resp, msg)
		cnReply <- &dnsReply{resp.Msg, code, err}
	}()
	// Prevent from passing the blocked query type to fallback upstreams.
	if c.opts.block > 0 && state.QType() == c.opts.block && state.QClass() == dns.ClassINET {
		r := new(dns.Msg)
		r.SetReply(&msgCopy)
		r.Authoritative = true
		fbReply <- &dnsReply{r, 0, nil}
	} else {
		go func() {
			resp := nonwriter.New(writer)
			code, err := plugin.NextOrFailure(c.Name(), c.Next, ctx, resp, &msgCopy)
			fbReply <- &dnsReply{resp.Msg, code, err}
		}()
	}
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
	c.RLock()
	defer c.RUnlock()
	for _, ans := range r.Answer {
		ips := make([]net.IP, 0, 1)
		switch ans.Header().Rrtype {
		case dns.TypeA:
			ips = append(ips, ans.(*dns.A).A)
		case dns.TypeAAAA:
			ips = append(ips, ans.(*dns.AAAA).AAAA)
		case dns.TypeHTTPS:
			ips = c.extractSVCBIPs(ans.(*dns.HTTPS).Value)
		case dns.TypeSVCB:
			ips = c.extractSVCBIPs(ans.(*dns.SVCB).Value)
		default:
			continue
		}
		var record struct {
			Country struct {
				ISOCode string `maxminddb:"iso_code"`
			} `maxminddb:"country"`
		}
		for _, ip := range ips {
			if err := c.geoIP.Lookup(ip, &record); err != nil {
				return false
			}
			if record.Country.ISOCode == "CN" {
				return true
			}
		}
	}
	return false
}

func (c *ChinaDNS) extractSVCBIPs(values []dns.SVCBKeyValue) []net.IP {
	var ips []net.IP
	var v4, v6 bool
	for _, v := range values {
		switch v := v.(type) {
		case *dns.SVCBIPv4Hint:
			ips = append(ips, v.Hint...)
			v4 = true
		case *dns.SVCBIPv6Hint:
			ips = append(ips, v.Hint...)
			v6 = true
		}
		if v4 && v6 {
			break
		}
	}
	return ips
}

func (c *ChinaDNS) bypass(name string) bool {
	if strings.HasSuffix(name, ".cn.") {
		return true
	}
	for _, ignore := range c.opts.ignored {
		if plugin.Name(ignore).Matches(name) {
			return true
		}
	}
	return false
}

func (c *ChinaDNS) readDB() error {
	file, err := os.Open(c.opts.path)
	if err != nil {
		return err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return err
	}
	c.RLock()
	size := c.size
	c.RUnlock()

	if c.mtime.Equal(stat.ModTime()) && size == stat.Size() {
		return nil
	}
	geoBytes, err := io.ReadAll(file)
	if err != nil {
		return err
	}
	geoIP, err := maxminddb.FromBytes(geoBytes)
	if err != nil {
		return err
	}
	c.Lock()
	if c.geoIP != nil {
		c.geoIP.Close()
		log.Infof("Reload complete with mod time = %s", stat.ModTime().String())
	}
	c.geoIP = geoIP
	// Update the data cache.
	c.mtime = stat.ModTime()
	c.size = stat.Size()
	c.Unlock()
	return nil
}

func (c *ChinaDNS) OnShutdown() error {
	c.RLock()
	defer c.RUnlock()
	return c.geoIP.Close()
}
