# ChinaDNS Plugin for CoreDNS

## Name

*chinadns* - fast and accurate dns for chinese users.

## Description

A CoreDns plugin that select a dns reply from two types of upstreams (main and fallback) with the following procedure:

1. Make concurrent dns requests to main and fallback upstreams.
2. Wait for a reply from main upstream.
3. Use GeoIP database to determine IP country of main reply. If the main reply contains a China IP, then the reply is
   selected and returned to client immediately. Otherwise, go to step 4.
4. Wait for a reply from fallback upstream and return it to client.

This plugin applies to both IPv4 and IPv6 addresses.

## Compilation

A simple way to compile this plugin, is by adding the following on [plugin.cfg](https://github.com/coredns/coredns/blob/master/plugin.cfg) __right after the `forward` plugin__,
and recompile it as [detailed on coredns.io](https://coredns.io/2017/07/25/compile-time-enabling-or-disabling-plugins/#build-with-compile-time-configuration-file).

```txt
# right after forward:forward
chinadns:github.com/koho/coredns-chinadns
```

After this you can compile coredns by:

```sh
go generate
go build
```

Or you can instead use make:

```sh
make
```

## Syntax

```txt
chinadns DBFILE TO... {
    fallback TO...
}
```

* **DBFILE** the mmdb database file path. We recommend updating your mmdb database periodically for more accurate results.
* **TO...** are the main destination endpoints to forward to. We usually add a dns server inside China to this list.
* `fallback` specifies the fallback upstream servers.
  * `TO...` are the fallback destination endpoints to forward to. We usually add a dns server outside China to this list.

## Examples

Common configuration.

```corefile
. {
  chinadns /etc/cn.mmdb 223.5.5.5 114.114.114.114 {
    fallback 8.8.8.8 tls://1.1.1.1
  }
}
```

In this configuration, we block AAAA query that outside China:

```corefile
.:5300 {
    bind 127.0.0.1
    template IN AAAA .
    forward . 8.8.8.8
}

. {
  chinadns /etc/cn.mmdb 223.5.5.5 {
    fallback 127.0.0.1:5300
  }
}
```
