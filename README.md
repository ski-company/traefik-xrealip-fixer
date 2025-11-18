# traefik-xrealip-fixer
Middleware Traefik qui détecte la véritable IP client derrière Cloudflare / ALB / proxies multiples en indexant X-Forwarded-For par la fin pour éviter le spoofing, puis génère correctement X-Real-IP.
