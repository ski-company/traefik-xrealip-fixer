![Traefik XRealIP Fixer Banner](https://raw.githubusercontent.com/ski-company/traefik-xrealip-fixer/master/.assets/traefik-xrealip-fixer-logo.png)

# Traefik XRealIP Fixer

Traefik middleware that rebuilds a trustworthy client IP by inspecting:
- Cloudflare / CloudFront headers,
- the remote socket,
- a controlled reverse scan of `X-Forwarded-For` (closest hop first, depth-limited).

Each request is marked with `X-Realip-Fixer-Trusted` (`yes`/`no`), `X-Realip-Fixer-Provider` (`cloudflare`/`cloudfront`/`direct`/`unknown`), and `X-Country` when GeoLite2 Country can resolve the validated IP. `X-Real-IP` / `X-Forwarded-For` are rewritten for downstream services.

## How it works
- No provider header (CF/CFN) → “direct” path: use socket IP or walk XFF up to `directDepth`.
- Provider header present → validate the *edge IP* from the XFF tail (up to `directDepth`) against Cloudflare/CloudFront CIDRs (periodically refreshed); fallback to socket IP only if no valid XFF hop; otherwise 410 Gone.
- Extract client IP from the provider header, fall back to socket IP if invalid, then rewrite XFF / X-Real-IP.
- When a MaxMind license key is configured, download GeoLite2 Country at startup, refresh it periodically when `autoRefresh` is enabled, and set `X-Country` to the validated IP country ISO code when available.

## Plugin configuration (dynamic.yml)
```yaml
http:
  middlewares:
    xrealip-fixer:
      plugin:
        xrealip-fixer:
          autoRefresh: true            # periodic refresh of CF/CFN CIDRs and GeoLite2 Country
          refreshInterval: 30m         # Go duration, e.g. "12h", "30m"
          directDepth: 1               # how many XFF hops to walk in direct mode
          geoLite2LicenseKey: ""       # optional: MaxMind license key
          geoLite2DownloadURL: ""      # optional: MaxMind-compatible download endpoint override
          trustip:                     # optional: extra CIDRs to trust per provider
            cloudflare:
              - "203.0.113.0/24"
            cloudfront:
              - "198.51.100.0/24"
          debug: false
```

### Headers added / rewritten
- `X-Real-IP`: validated client IP.
- `X-Forwarded-For`: append validated client IP.
- `X-Realip-Fixer-Trusted`: `yes` or `no`.
- `X-Realip-Fixer-Provider`: `cloudflare`, `cloudfront`, `direct`, `unknown`.
- `X-Country`: ISO country code from MaxMind GeoLite2 Country for the validated client IP, when available.

### Response codes
- Provider header present but socket IP not allowed → `410 Gone` + provider headers stripped.

## Local Traefik example (excerpt)
Static (`traefik-test/traefik.yml`) to enable local plugin:
```yaml
experimental:
  localPlugins:
    xrealip-fixer:
      moduleName: github.com/ski-company/traefik-xrealip-fixer
```
Dynamic (`traefik-test/dynamic.yml`):
```yaml
http:
  middlewares:
    xrealip-fixer:
      plugin:
        xrealip-fixer:
          autoRefresh: true
          refreshInterval: 30m
          directDepth: 1
          geoLite2LicenseKey: ""
          debug: false

  routers:
    whoami-router:
      rule: Host(`whoami.local`)
      entryPoints: [web]
      service: whoami-svc
      middlewares: [xrealip-fixer]
```

## Dev / local test
- `docker compose -f docker-compose-test.yml up -d` (Traefik + whoami + plugin source mounted).
- k6 benchmark (profile `bench`):  
  `docker compose -f docker-compose-test.yml --profile bench run --rm k6`  
  Optional env: `HOST=whoami.local`, `TARGET_URL=http://traefik/`, `XFF="203.0.113.10, 10.0.0.1"`, `VUS`, `DURATION`.

## Config fields (struct `Config`)
- `trustip`: map provider → extra CIDRs.
- `autoRefresh` (bool), `refreshInterval` (Go duration).
- `directDepth` (int): XFF depth for direct path.
- `geoLite2LicenseKey`: MaxMind license key used to download GeoLite2 Country.
- `geoLite2DownloadURL`: optional MaxMind-compatible endpoint override. If empty, MaxMind's default endpoint is used.
- `debug` (bool).

## Licence
MIT

---

See `README.fr.md` for the French version.
