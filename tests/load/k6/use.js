import http from 'k6/http';
import { check, fail } from 'k6';
import { Counter } from 'k6/metrics';

const failByStatus = new Counter('veil_fail_status');

const parsedVus = Number.parseInt(__ENV.VEIL_VUS || '50', 10);
const vus = Number.isInteger(parsedVus) && parsedVus > 0 ? parsedVus : 50;

export const options = {
  stages: [
    { duration: '30s', target: Math.max(1, Math.floor(vus / 5)) },
    { duration: '1m', target: vus },
    { duration: '30s', target: 0 },
  ],
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<1000'],
    checks: ['rate==1'],
  },
};

const rawOrigins = __ENV.VEIL_ORIGINS || __ENV.VEIL_ORIGIN || 'http://127.0.0.1:8080';
const origins = rawOrigins.split(',').map((s) => s.trim()).filter(Boolean);
const rawTokens = __ENV.VEIL_TOKENS || __ENV.VEIL_AGENT_TOKEN;
const tokens = rawTokens ? rawTokens.split(',').map((s) => s.trim()).filter(Boolean) : [];
// Multi-org runs pass VEIL_ITEM_IDS parallel to VEIL_TOKENS — the session's
// org owns its item. Single-org runs keep the single VEIL_ITEM_ID.
const rawItems = __ENV.VEIL_ITEM_IDS || __ENV.VEIL_ITEM_ID;
const items = rawItems ? rawItems.split(',').map((s) => s.trim()).filter(Boolean) : [];
const upstream = __ENV.VEIL_UPSTREAM_URL || 'https://httpbin.org/get';

export default function () {
  if (tokens.length === 0 || items.length === 0) {
    fail('VEIL_TOKENS (or VEIL_AGENT_TOKEN) and VEIL_ITEM_ID(S) are required');
  }

  const tokenIdx = (Number(__VU) - 1) % tokens.length;
  const token = tokens[tokenIdx];
  const item = items[Math.min(tokenIdx, items.length - 1)];

  const idx = (Number(__VU) + Number(__ITER)) % origins.length;
  const origin = origins[idx];

  const res = http.post(
    `${origin}/v1/use`,
    JSON.stringify({ item, url: upstream, method: 'GET' }),
    {
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
      timeout: '15s',
    }
  );

  check(res, {
    'status is 200': (r) => r.status === 200,
    'decision is allow': (r) => {
      try {
        return r.json('decision') === 'allow';
      } catch {
        return false;
      }
    },
  });
  if (res.status !== 200) {
    failByStatus.add(1, { status: String(res.status) });
    if (__VU <= 10) {
      console.log(`fail vu=${__VU} status=${res.status} body=${String(res.body).slice(0, 160)}`);
    }
  }
}
