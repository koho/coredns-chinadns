package chinadns

import (
	"crypto/tls"
	"fmt"
	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/forward"
	"github.com/coredns/coredns/plugin/pkg/parse"
	"github.com/coredns/coredns/plugin/pkg/transport"
	"github.com/oschwald/maxminddb-golang"
)

func init() { plugin.Register("chinadns", setup) }

func setup(c *caddy.Controller) error {
	f, err := parseChinaDNS(c)
	if err != nil {
		return err
	}
	cnProxies := c.Get("cn").([]*forward.Proxy)
	fbProxies := c.Get("fb").([]*forward.Proxy)
	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		f.Next = next
		return f
	})
	c.OnStartup(func() error {
		for _, proxy := range cnProxies {
			f.cnFwd.SetProxy(proxy)
		}
		for _, proxy := range fbProxies {
			f.fbFwd.SetProxy(proxy)
		}
		return nil
	})
	c.OnShutdown(f.cnFwd.OnShutdown)
	c.OnShutdown(f.fbFwd.OnShutdown)
	return nil
}

func parseChinaDNS(c *caddy.Controller) (*ChinaDNS, error) {
	f := New()
	var dbPath string
	var err error

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
			if err = parseBlock(c); err != nil {
				return nil, err
			}
		}
	}
	if f.geoIP, err = maxminddb.Open(dbPath); err != nil {
		return nil, plugin.Error("chinadns", err)
	}
	return f, nil
}

func parseBlock(c *caddy.Controller) error {
	switch c.Val() {
	case "fallback":
		proxies, err := parseProxy(c, c.RemainingArgs())
		if err != nil {
			return err
		}
		c.Set("fb", proxies)
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
		proxies = append(proxies, p)
	}
	return proxies, nil
}
