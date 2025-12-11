<p align="left">
  <img src=".assets/traefik-xrealip-fixer-logo-transparent.png" alt="traefik-xrealip-fixer logo" width="220" />
</p>

# traefik-xrealip-fixer

Middleware Traefik qui reconstruit l’IP client de façon fiable en fonction :
- des en-têtes Cloudflare (CF-Connecting-IP) et CloudFront (Cloudfront-Viewer-Address),
- du socket distant,
- d’un scan contrôlé de `X-Forwarded-For` depuis la fin (proche du dernier proxy).

Chaque requête est marquée via `X-Realip-Fixer-Trusted` (yes/no) et `X-Realip-Fixer-Provider` (cloudflare/cloudfront/direct/unknown), et `X-Real-IP` / `X-Forwarded-For` sont réécrits pour les services en aval.

## Fonctionnement
- Pas d’en-tête provider (CF/CFN) : chemin direct, on prend l’IP socket ou un hop XFF selon `directDepth`.
- En-tête provider présent : on vérifie que l’IP socket appartient aux ranges Cloudflare/CloudFront (CIDRs rafraîchies périodiquement). Sinon 410 Gone.
- On extrait l’IP client à partir du header provider, fallback IP socket si invalide, puis on réécrit XFF / X-Real-IP.

## Configuration du plugin (dynamic.yml)
```yaml
http:
  middlewares:
    xrealip-fixer:
      plugin:
        xrealip-fixer:
          autoRefresh: true            # refresh périodique des CIDRs CF/CFN
          refreshInterval: 30m         # durée Go, ex: "12h", "30m"
          directDepth: 1               # nombre de hops XFF à considérer en direct
          trustip:                     # (optionnel) CIDRs custom par provider
            cloudflare:
              - "203.0.113.0/24"
            cloudfront:
              - "198.51.100.0/24"
          debug: false
```

### Headers ajoutés / réécrits
- `X-Real-IP` : IP client validée.
- `X-Forwarded-For` : append de l’IP client validée.
- `X-Realip-Fixer-Trusted` : `yes` ou `no`.
- `X-Realip-Fixer-Provider` : `cloudflare`, `cloudfront`, `direct`, `unknown`.

### Codes de réponse
- Header provider présent mais IP socket non autorisée → 410 Gone + headers provider nettoyés.

## Exemple Traefik local (extrait)
Static (`traefik-test/traefik.yml`) pour activer le plugin local :
```yaml
experimental:
  localPlugins:
    xrealip-fixer:
      moduleName: github.com/ski-company/traefik-xrealip-fixer
```
Dynamic (`traefik-test/dynamic.yml`) :
```yaml
http:
  middlewares:
    xrealip-fixer:
      plugin:
        xrealip-fixer:
          autoRefresh: true
          refreshInterval: 30m
          directDepth: 1
          debug: false

  routers:
    whoami-router:
      rule: Host(`whoami.local`)
      entryPoints: [web]
      service: whoami-svc
      middlewares: [xrealip-fixer]
```

## Dev / bench local
- `docker compose -f docker-compose-test.yml up -d` (Traefik + whoami + plugin monté).
- k6 (profil `bench`) :  
  `docker compose -f docker-compose-test.yml --profile bench run --rm k6`  
  Env optionnelles : `HOST=whoami.local`, `TARGET_URL=http://traefik/`, `XFF="203.0.113.10, 10.0.0.1"`, `VUS`, `DURATION`.

## Champs de configuration (struct `Config`)
- `trustip` : map provider → CIDRs additionnelles.
- `autoRefresh` (bool), `refreshInterval` (durée Go).
- `directDepth` (int) : profondeur XFF en chemin direct.
- `debug` (bool).

## Licence
MIT
