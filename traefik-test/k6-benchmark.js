import http from "k6/http";
import { check, sleep } from "k6";
import { Counter, Trend } from "k6/metrics";

// Custom metrics
const geoipHits   = new Counter("geoip_header_present");
const geoipMisses = new Counter("geoip_header_absent");
const pluginLat   = new Trend("plugin_latency_ms", true);

export const options = {
  stages: [
    { duration: "10s", target: 20  }, // warm-up
    { duration: "30s", target: 50  }, // sustained load
    { duration: "20s", target: 100 }, // stress
    { duration: "10s", target: 200 }, // peak spike
    { duration: "10s", target: 0   }, // ramp-down
  ],
  thresholds: {
    // ~10% of requests are intentional 410s (spoofed CDN) → allow up to 12%
    http_req_failed:   ["rate<0.12"],
    http_req_duration: ["p(95)<300"],
    plugin_latency_ms: ["p(99)<500"],
    // All scenario checks must pass (status + header presence)
    checks:            ["rate>0.99"],
  },
};

const BASE = __ENV.TARGET_URL || "http://traefik/";

const HOSTS = ["whoami1.local", "whoami2.local"];

// ── Public IPs spread across many countries (for GeoIP coverage) ──────────────
const PUBLIC_IPS = [
  // North America
  "8.8.8.8",          // US – Google DNS
  "4.4.4.4",          // US – Level3
  "208.67.222.222",   // US – OpenDNS
  "45.33.32.156",     // US – Linode
  "162.248.16.1",     // CA
  // Europe
  "77.88.8.8",        // RU – Yandex
  "194.109.6.66",     // NL
  "80.67.169.12",     // FR – FDN
  "195.148.127.1",    // FR
  "185.220.101.1",    // DE
  "62.216.0.1",       // GB
  "212.58.244.70",    // GB – BBC
  "91.108.4.1",       // NL – Telegram
  // Asia-Pacific
  "1.1.1.1",          // AU – Cloudflare
  "1.0.0.1",          // AU – Cloudflare
  "43.156.0.1",       // CN – Tencent
  "114.114.114.114",  // CN – 114DNS
  "101.226.4.6",      // CN – Alibaba
  "202.12.27.33",     // JP – WIDE
  "61.8.0.1",         // ID
  "122.56.0.1",       // NZ
  // Middle-East / Africa
  "41.191.64.1",      // ZA
  "196.201.216.1",    // KE
  "37.49.224.1",      // IR
  // Latin America
  "200.221.11.100",   // BR – Embratel
  "186.64.0.1",       // AR
  // IPv6 public addresses
  "2001:4860:4860::8888",    // US – Google DNS v6
  "2606:4700:4700::1111",    // US – Cloudflare v6
  "2a00:1450:4001:81e::200e",// DE – Google v6
  "2001:41d0:1:1b00::1",    // FR – OVH v6
];

// ── Cloudflare edge IPs (from official published ranges) ──────────────────────
const CF_EDGE_IPS = [
  "103.21.244.1",
  "103.22.200.1",
  "103.31.4.1",
  "141.101.64.1",
  "108.162.192.1",
  "190.93.240.1",
  "188.114.96.1",
  "197.234.240.1",
  "198.41.128.1",
  "162.158.0.1",
  "104.16.0.1",
  "104.24.0.1",
  "172.64.0.1",
  "131.0.72.1",
];

// ── CloudFront edge IPs (from AWS IP ranges, CLOUDFRONT prefix) ───────────────
const CFN_EDGE_IPS = [
  "13.32.0.1",
  "13.35.0.1",
  "204.246.164.1",
  "204.246.168.1",
  "52.46.0.1",
  "52.84.0.1",
  "54.182.0.1",
  "99.84.0.1",
  "130.176.0.1",
  "143.204.0.1",
];

function pick(arr) {
  return arr[Math.floor(Math.random() * arr.length)];
}

// Build a realistic XFF chain: 0-2 intermediate hops + edge IP at the end
function makeXFF(edgeIP) {
  const hops = Math.floor(Math.random() * 3);
  const chain = [];
  for (let i = 0; i < hops; i++) {
    chain.push(pick(PUBLIC_IPS));
  }
  chain.push(edgeIP);
  return chain.join(", ");
}

// ── Scenarios ─────────────────────────────────────────────────────────────────

function scenarioDirect() {
  const clientIP = pick(PUBLIC_IPS);
  return {
    headers: {
      Host: pick(HOSTS),
      "X-Forwarded-For": clientIP,
    },
    expect200: true,
  };
}

function scenarioCloudflare() {
  const clientIP = pick(PUBLIC_IPS);
  const edgeIP   = pick(CF_EDGE_IPS);
  return {
    headers: {
      Host: pick(HOSTS),
      "CF-Connecting-IP": clientIP,
      "X-Forwarded-For": makeXFF(edgeIP),
    },
    expect200: true,
  };
}

function scenarioCloudFront() {
  const clientIP = pick(PUBLIC_IPS);
  const edgeIP   = pick(CFN_EDGE_IPS);
  return {
    headers: {
      Host: pick(HOSTS),
      "Cloudfront-Viewer-Address": `${clientIP}:12345`,
      "X-Forwarded-For": makeXFF(edgeIP),
    },
    expect200: true,
  };
}

function scenarioSpoofedCDN() {
  // CF header present but edge IP is NOT in Cloudflare ranges → expect 410
  return {
    headers: {
      Host: pick(HOSTS),
      "CF-Connecting-IP": pick(PUBLIC_IPS),
      "X-Forwarded-For": "10.0.0.1, 192.168.1.1",
    },
    expect200: false,
  };
}

const SCENARIOS = [
  { fn: scenarioDirect,     weight: 40 },
  { fn: scenarioCloudflare, weight: 30 },
  { fn: scenarioCloudFront, weight: 20 },
  { fn: scenarioSpoofedCDN, weight: 10 },
];

const TOTAL_WEIGHT = SCENARIOS.reduce((s, sc) => s + sc.weight, 0);

function pickScenario() {
  let r = Math.random() * TOTAL_WEIGHT;
  for (const sc of SCENARIOS) {
    r -= sc.weight;
    if (r <= 0) return sc.fn();
  }
  return SCENARIOS[0].fn();
}

// ── Main VU loop ──────────────────────────────────────────────────────────────

export default function () {
  const { headers, expect200 } = pickScenario();

  const start = Date.now();
  const res = http.get(BASE, { headers });
  pluginLat.add(Date.now() - start);

  check(res, {
    "status matches scenario": (r) =>
      expect200 ? r.status === 200 : r.status === 410,
    // whoami echoes request headers in the body — check body, not response headers
    "X-Real-Ip set by plugin": (r) =>
      r.status !== 200 || (r.body !== null && r.body.includes("X-Real-Ip:")),
  });

  if (res.status === 200 && res.body !== null) {
    if (res.body.includes("X-Country:")) {
      geoipHits.add(1);
    } else {
      geoipMisses.add(1);
    }
  }

  // No sleep — maximize pressure on the plugin
}
