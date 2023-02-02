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

A simple way to compile this plugin, is by adding the following on [plugin.cfg](https://github.com/coredns/coredns/blob/master/plugin.cfg) __right before the `forward` plugin__,
and recompile it as [detailed on coredns.io](https://coredns.io/2017/07/25/compile-time-enabling-or-disabling-plugins/#build-with-compile-time-configuration-file).

```txt
# right before forward:forward
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
    block QUERY_TYPE
    reload DURATION
    except IGNORED_NAMES...
}
```

* **DBFILE** the mmdb database file path. We recommend updating your mmdb database periodically for more accurate results.
* **TO...** are the main destination endpoints to forward to. We usually add a dns server inside China to this list.
* `block` specifies the query type blocked from fallback upstreams. It generates an empty answer list for this type as the response of fallback upstreams.
* `reload` change the period between each database file reload. A time of zero seconds disables the feature.
  Examples of valid durations: "300ms", "1.5h" or "2h45m". See Go's [time](https://godoc.org/time) package. Default is 30s.
* **IGNORED_NAMES** in `except` is a space-separated list of domains to exclude from forwarding to fallback upstreams.
  Requests that match one of these names will be only forwarded to main upstreams.

## Examples

Common configuration.

```corefile
. {
  chinadns /etc/cn.mmdb 223.5.5.5 114.114.114.114
  forward . 8.8.8.8 tls://1.1.1.1
}
```

In this configuration, we block AAAA query that outside China:

```corefile
. {
  chinadns /etc/cn.mmdb 223.5.5.5 {
    block AAAA
  }
  forward . 8.8.8.8
}
```

In this configuration, we don't forward `example.com` to the fallback upstream `8.8.8.8`:

```corefile
. {
  chinadns /etc/cn.mmdb 223.5.5.5 {
    except example.com
  }
  forward . 8.8.8.8
}
```
