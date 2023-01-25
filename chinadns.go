package chinadns

import (
	"context"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/forward"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/plugin/pkg/nonwriter"
	"github.com/miekg/dns"
	"github.com/oschwald/maxminddb-golang"
	"net"
	"os"
	"sync"
	"time"
)

var log = clog.NewWithPlugin("chinadns")

type dnsReply struct {
	reply *dns.Msg
	code  int
	err   error
}

type options struct {
	// The path of GeoIP database
	path string
	// The time between two reload of the db file
	reload time.Duration
}

func newOptions() *options {
	return &options{
		reload: 30 * time.Second,
	}
}

type ChinaDNS struct {
	sync.RWMutex
	cnFwd *forward.Forward
	fbFwd *forward.Forward
	geoIP *maxminddb.Reader
	// mtime and size are only read and modified by a single goroutine
	mtime time.Time
	size  int64
	opts  *options
	Next  plugin.Handler
}

func New() *ChinaDNS {
	return &ChinaDNS{
		cnFwd: forward.New(),
		fbFwd: forward.New(),
		opts:  newOptions(),
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
	c.RLock()
	defer c.RUnlock()
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

func (c *ChinaDNS) readDB() error {
	file, err := os.Open(c.opts.path)
	if err != nil {
		return err
	}
	stat, err := file.Stat()
	file.Close()
	if err != nil {
		return err
	}
	c.RLock()
	size := c.size
	c.RUnlock()

	if c.mtime.Equal(stat.ModTime()) && size == stat.Size() {
		return nil
	}
	geoIP, err := maxminddb.Open(c.opts.path)
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
