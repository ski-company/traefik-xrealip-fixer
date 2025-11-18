# traefik-xrealip-fixer 

**traefik-xrealip-fixer** is a Traefik middleware that reliably reconstructs the true client IP address in environments where multiple proxies, CDNs, and load balancers interfere with or override IP-related headers.

Modern infrastructures often include layers such as Cloudflare, AWS ALB/NLB, Traefik ingress controllers, reverse proxies, and internal mesh components. Each hop may append or modify values in `X-Forwarded-For` or `X-Real-IP`, making it difficult — and sometimes impossible — for backend services to know the actual originating client IP.

This middleware solves that problem by implementing a robust, anti-spoofing IP extraction algorithm:

- Automatically handles Cloudflare headers (`CF-Connecting-IP`, `True-Client-IP`)
- Fully compatible with AWS ALB/NLB and other proxy layers
- Extracts the correct client IP from `X-Forwarded-For` using **reverse indexing** (from the end of the list)
- Ignores spoofed, private, reserved, and internal IP ranges
- Overwrites or sets `X-Real-IP` with a clean, verified public IP address

By restoring an accurate and trustworthy client source address, `traefik-xrealip-fixer` improves:
- access logs consistency  
- rate limiting precision  
- WAF and firewall rule reliability  
- fraud detection  
- security auditing  
- any IP-based decision system  

This plugin makes your applications “real IP aware” again — even behind complex proxy chains.
