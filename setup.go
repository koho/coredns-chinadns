package chinadns

import (
	"crypto/tls"
	"fmt"
	"time"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/forward"
	"github.com/coredns/coredns/plugin/pkg/parse"
	"github.com/coredns/coredns/plugin/pkg/transport"

	"github.com/miekg/dns"
)

func init() { plugin.Register("chinadns", setup) }

func setup(c *caddy.Controller) error {
	cd, err := parseChinaDNS(c)
	if err != nil {
		return err
	}
	cnProxies := c.Get("cn").([]*forward.Proxy)
	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		cd.Next = next
		return cd
	})
	updateChan := periodicDBUpdate(cd)
	c.OnStartup(func() error {
		for _, proxy := range cnProxies {
			cd.fwd.SetProxy(proxy)
		}
		return nil
	})
	c.OnShutdown(cd.fwd.OnShutdown)
	c.OnShutdown(func() error {
		close(updateChan)
		return nil
	})
	c.OnShutdown(cd.OnShutdown)
	return nil
}

func parseChinaDNS(c *caddy.Controller) (*ChinaDNS, error) {
	cd := New()
	var dbPath string

	for c.Next() {
		if !c.NextArg() {
			return nil, c.ArgErr()
		}
		if dbPath != "" {
			return nil, c.Errf("configuring multiple databases is not supported")
		}
		dbPath = c.Val()
		proxies, err := parseProxy(c, c.RemainingArgs())
		if err != nil {
			return nil, err
		}
		c.Set("cn", proxies)

		for c.NextBlock() {
			if err = parseBlock(c, cd); err != nil {
				return nil, err
			}
		}
	}
	cd.opts.path = dbPath
	if err := cd.readDB(); err != nil {
		return nil, plugin.Error("chinadns", err)
	}
	return cd, nil
}

func parseBlock(c *caddy.Controller, cd *ChinaDNS) error {
	switch c.Val() {
	case "except":
		ignore := c.RemainingArgs()
		if len(ignore) == 0 {
			return c.ArgErr()
		}
		for i := 0; i < len(ignore); i++ {
			cd.opts.ignored = append(cd.opts.ignored, plugin.Host(ignore[i]).NormalizeExact()...)
		}
	case "reload":
		remaining := c.RemainingArgs()
		if len(remaining) != 1 {
			return c.Errf("reload needs a duration (zero seconds to disable)")
		}
		reload, err := time.ParseDuration(remaining[0])
		if err != nil {
			return c.Errf("invalid duration for reload '%s'", remaining[0])
		}
		if reload < 0 {
			return c.Errf("invalid negative duration for reload '%s'", remaining[0])
		}
		cd.opts.reload = reload
	case "block":
		if !c.NextArg() {
			return c.ArgErr()
		}
		qtype, ok := dns.StringToType[c.Val()]
		if !ok {
			return c.Errf("invalid RR class %s", c.Val())
		}
		cd.opts.block = qtype
	default:
		return c.Errf("unknown property '%s'", c.Val())
	}
	return nil
}

func parseProxy(c *caddy.Controller, to []string) ([]*forward.Proxy, error) {
	if len(to) == 0 {
		return nil, c.ArgErr()
	}
	toHosts, err := parse.HostPortOrFile(to...)
	if err != nil {
		return nil, err
	}
	proxies := make([]*forward.Proxy, 0)
	allowedTrans := map[string]bool{"dns": true, "tls": true}
	tlsConfig := &tls.Config{ClientSessionCache: tls.NewLRUClientSessionCache(len(toHosts))}
	for _, host := range toHosts {
		trans, h := parse.Transport(host)

		if !allowedTrans[trans] {
			return nil, fmt.Errorf("'%s' is not supported as a destination protocol in chinadns: %s", trans, host)
		}
		p := forward.NewProxy(h, trans)
		if trans == transport.TLS {
			p.SetTLSConfig(tlsConfig)
		}
		p.SetExpire(10 * time.Second)
		proxies = append(proxies, p)
	}
	return proxies, nil
}

func periodicDBUpdate(cd *ChinaDNS) chan bool {
	updateChan := make(chan bool)

	if cd.opts.reload == 0 {
		return updateChan
	}

	go func() {
		ticker := time.NewTicker(cd.opts.reload)
		defer ticker.Stop()
		for {
			select {
			case <-updateChan:
				return
			case <-ticker.C:
				cd.readDB()
			}
		}
	}()
	return updateChan
}
