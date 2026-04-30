![Traefik XRealIP Fixer Banner](https://raw.githubusercontent.com/ski-company/traefik-xrealip-fixer/master/.assets/traefik-xrealip-fixer-logo.png)

# Traefik XRealIP Fixer

Middleware Traefik qui reconstruit l’IP client de façon fiable en fonction :
- des en-têtes Cloudflare (CF-Connecting-IP) et CloudFront (Cloudfront-Viewer-Address),
- du socket distant,
- d’un scan contrôlé de `X-Forwarded-For` depuis la fin (proche du dernier proxy).

Chaque requête est marquée via `X-Realip-Fixer-Trusted` (yes/no), `X-Realip-Fixer-Provider` (cloudflare/cloudfront/direct/unknown), et `X-Country` quand GeoLite2 Country peut résoudre l’IP validée. `X-Real-IP` / `X-Forwarded-For` sont réécrits pour les services en aval.

## Fonctionnement
- Pas d’en-tête provider (CF/CFN) : chemin direct, on prend l’IP socket ou un hop XFF selon `directDepth`.
- En-tête provider présent : on valide l’IP « edge » depuis la fin du XFF (limité par `directDepth`) contre les CIDRs Cloudflare/CloudFront (rafraîchies périodiquement). On ne retombe sur l’IP socket que si aucun hop XFF valide, sinon 410 Gone.
- On extrait l’IP client à partir du header provider, fallback IP socket si invalide, puis on réécrit XFF / X-Real-IP.
- Quand une clé MaxMind est configurée, on télécharge GeoLite2 Country au démarrage, on la rafraîchit périodiquement si `autoRefresh` est activé, puis on remplit `X-Country` avec le code ISO pays de l’IP validée quand disponible.

## Configuration du plugin (dynamic.yml)
```yaml
http:
  middlewares:
    xrealip-fixer:
      plugin:
        xrealip-fixer:
          autoRefresh: true            # refresh périodique des CIDRs CF/CFN et de GeoLite2 Country
          refreshInterval: 30m         # durée Go, ex: "12h", "30m"
          directDepth: 1               # nombre de hops XFF à considérer en direct
          geoLite2LicenseKey: ""       # optionnel: clé MaxMind
          geoLite2DownloadURL: ""      # optionnel: endpoint de téléchargement compatible MaxMind
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
- `X-Country` : code ISO pays MaxMind GeoLite2 Country pour l’IP client validée, quand disponible.

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
          geoLite2LicenseKey: ""
          debug: false

  routers:
    whoami-router:
      rule: Host(`whoami.local`)
      entryPoints: [web]
      service: whoami-svc
      middlewares: [xrealip-fixer]
```

## Dev / bench local
- `docker compose -f docker-compose-test.yml up -d` (Traefik + whoami1 + whoami2 + plugin monté).
- k6 (profil `bench`) :  
  `docker compose -f docker-compose-test.yml --profile bench run --rm k6`  
  Le script exécute des stages de charge fixes (warm-up → pic 200 VUs). Env optionnelles : `TARGET_URL=http://traefik/`, `XFF="203.0.113.10, 10.0.0.1"`.

## Champs de configuration (struct `Config`)
- `trustip` : map provider → CIDRs additionnelles.
- `autoRefresh` (bool), `refreshInterval` (durée Go).
- `directDepth` (int) : profondeur XFF en chemin direct.
- `geoLite2LicenseKey` : clé MaxMind utilisée pour télécharger GeoLite2 Country.
- `geoLite2DownloadURL` : endpoint compatible MaxMind optionnel. Si vide, l’endpoint MaxMind par défaut est utilisé.
- `debug` (bool).

## Licence
MIT
