// Behavioral agent load test — models an agent working, not a request cannon.
// Each VU is one seeded agent session looping: list items, think, use a
// credential, think. Think-time makes "N VUs" mean "N agents actively working"
// (~0.1-0.2 req/s each), the way the reference methodology defines a user.
//
// SLO gate (same bar as the reference run): p95 < 500ms, p99 < 1s, <1% errors.
// A level "passes" if all thresholds hold for the whole steady-state window.
import http from 'k6/http';
import { check, fail, sleep } from 'k6';
import { Counter } from 'k6/metrics';

const failByStatus = new Counter('veil_fail_status');

const vus = Number.parseInt(__ENV.VEIL_VUS || '50', 10);
const rampSec = Number.parseInt(__ENV.VEIL_RAMP_S || '30', 10);
const holdSec = Number.parseInt(__ENV.VEIL_HOLD_S || '120', 10);

export const options = {
  scenarios: {
    agents: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: `${rampSec}s`, target: vus },
        { duration: `${holdSec}s`, target: vus },
        { duration: '30s', target: 0 },
      ],
      gracefulRampDown: '10s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    'http_req_duration{name:list}': ['p(95)<500', 'p(99)<1000'],
    'http_req_duration{name:use}': ['p(95)<500', 'p(99)<1000'],
  },
};

const origins = (__ENV.VEIL_ORIGINS || __ENV.VEIL_ORIGIN || 'https://veil.nyc')
  .split(',').map((s) => s.trim()).filter(Boolean);

let tokens = [];
if (__ENV.VEIL_TOKENS_FILE) {
  tokens = open(__ENV.VEIL_TOKENS_FILE).split('\n').map((s) => s.trim()).filter(Boolean);
} else if (__ENV.VEIL_TOKENS || __ENV.VEIL_AGENT_TOKEN) {
  tokens = (__ENV.VEIL_TOKENS || __ENV.VEIL_AGENT_TOKEN).split(',').map((s) => s.trim()).filter(Boolean);
}

let items = [];
if (__ENV.VEIL_ITEMS_FILE) {
  items = open(__ENV.VEIL_ITEMS_FILE).split('\n').map((s) => s.trim()).filter(Boolean);
} else if (__ENV.VEIL_ITEM_IDS || __ENV.VEIL_ITEM_ID) {
  items = (__ENV.VEIL_ITEM_IDS || __ENV.VEIL_ITEM_ID).split(',').map((s) => s.trim()).filter(Boolean);
}

const upstream = __ENV.VEIL_UPSTREAM_URL || 'https://loadtest-echo.veil.nyc/';

export default function () {
  if (tokens.length === 0 || items.length === 0) {
    fail('VEIL_TOKENS_FILE (or VEIL_TOKENS) and VEIL_ITEMS_FILE (or VEIL_ITEM_IDS) are required');
  }

  const idx = (Number(__VU) - 1) % tokens.length;
  const token = tokens[idx];
  const item = items[Math.min(idx, items.length - 1)];
  const origin = origins[(Number(__VU) + Number(__ITER)) % origins.length];
  const headers = { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` };

  // Agents enumerate their granted items on wake and occasionally re-list.
  const list = http.get(`${origin}/v1/items`, { headers, tags: { name: 'list' }, timeout: '15s' });
  check(list, { 'list 200': (r) => r.status === 200 });
  record(list);

  sleep(1 + Math.random() * 3); // think 1-4s

  const use = http.post(
    `${origin}/v1/use`,
    JSON.stringify({ item, url: upstream, method: 'GET' }),
    { headers, tags: { name: 'use' }, timeout: '15s' }
  );
  check(use, {
    'use 200': (r) => r.status === 200,
    'use allow': (r) => {
      try { return r.json('decision') === 'allow'; } catch { return false; }
    },
  });
  record(use);

  // ~10% of loops burst a second use immediately (agents that chain calls).
  if (Math.random() < 0.1) {
    const again = http.post(
      `${origin}/v1/use`,
      JSON.stringify({ item, url: upstream, method: 'GET' }),
      { headers, tags: { name: 'use' }, timeout: '15s' }
    );
    check(again, { 'use 200': (r) => r.status === 200 });
    record(again);
  }

  sleep(4 + Math.random() * 8); // think 4-12s → ~2-3 req per ~10-17s loop
}

function record(res) {
  if (res.status >= 400 || res.status === 0) {
    failByStatus.add(1, { status: String(res.status) });
    if (__VU <= 5) {
      console.log(`fail vu=${__VU} status=${res.status} body=${String(res.body).slice(0, 160)}`);
    }
  }
}
