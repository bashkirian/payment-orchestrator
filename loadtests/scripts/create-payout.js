import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Trend } from 'k6/metrics';
import { generateIdempotencyKey, randomPayoutPayload } from '../lib/helpers.js';

// Custom metrics
const payoutCreated = new Counter('payouts_created');
const payoutFailed = new Counter('payouts_failed');
const payoutLatency = new Trend('payout_latency');

// Test configuration
export const options = {
  stages: [
    { duration: '30s', target: 50 },   // Ramp up to 50 VUs
    { duration: '1m', target: 50 },    // Stay at 50 VUs
    { duration: '30s', target: 100 },  // Spike to 100 VUs
    { duration: '1m', target: 100 },   // Stay at 100 VUs
    { duration: '30s', target: 0 },    // Ramp down
  ],
  thresholds: {
    http_req_duration: ['p(95)<200'],  // 95% of requests < 200ms
    http_req_failed: ['rate<0.01'],    // Error rate < 1%
    payout_latency: ['p(95)<200'],
  },
};

const BASE_URL = __ENV.API_URL || 'http://localhost:8080';

export default function () {
  const idempotencyKey = generateIdempotencyKey();
  const payload = randomPayoutPayload('card');

  const params = {
    headers: {
      'Content-Type': 'application/json',
      'Idempotency-Key': idempotencyKey,
    },
  };

  const startTime = new Date();
  const response = http.post(`${BASE_URL}/v1/payouts`, JSON.stringify(payload), params);
  const duration = new Date() - startTime;

  payoutLatency.add(duration);

  const success = check(response, {
    'status is 202': (r) => r.status === 202,
    'has payout_id': (r) => {
      try {
        const body = JSON.parse(r.body);
        return body.payout_id !== undefined;
      } catch {
        return false;
      }
    },
  });

  if (success) {
    payoutCreated.add(1);
  } else {
    payoutFailed.add(1);
  }

  // Small think time between requests
  sleep(0.1);
}

export function handleSummary(data) {
  return {
    stdout: textSummary(data, { indent: ' ', enableColors: true }),
    'loadtests/results/create-payout-summary.json': JSON.stringify(data, null, 2),
  };
}

function textSummary(data, options = {}) {
  const indent = options.indent || '  ';
  const colors = options.enableColors || false;

  const colorize = (str, color) => colors ? str : str;

  let summary = '\n' + colorize('=', 'cyan') + '\n';
  summary += colorize('  CREATE PAYOUT LOAD TEST SUMMARY', 'cyan') + '\n';
  summary += colorize('=', 'cyan') + '\n\n';

  // HTTP metrics
  const httpReqs = data.metrics.http_reqs?.values?.count || 0;
  const httpReqDuration = data.metrics.http_req_duration?.values || {};
  const httpReqFailed = data.metrics.http_req_failed?.values?.rate || 0;

  summary += `  Total Requests:     ${httpReqs}\n`;
  summary += `  Error Rate:         ${(httpReqFailed * 100).toFixed(2)}%\n`;
  summary += `  Latency (p50):      ${(httpReqDuration['p(50)'] || 0).toFixed(2)}ms\n`;
  summary += `  Latency (p95):      ${(httpReqDuration['p(95)'] || 0).toFixed(2)}ms\n`;
  summary += `  Latency (p99):      ${(httpReqDuration['p(99)'] || 0).toFixed(2)}ms\n\n`;

  // Custom metrics
  const created = data.metrics.payouts_created?.values?.count || 0;
  const failed = data.metrics.payouts_failed?.values?.count || 0;

  summary += `  Payouts Created:    ${created}\n`;
  summary += `  Payouts Failed:     ${failed}\n\n`;

  // Thresholds
  summary += colorize('  THRESHOLDS', 'yellow') + '\n';
  const thresholds = data.metrics.http_req_duration?.thresholds || {};
  const durationThreshold = thresholds['p(95)<200'];
  if (durationThreshold) {
    const passed = durationThreshold.ok ? colorize('PASS', 'green') : colorize('FAIL', 'red');
    summary += `    http_req_duration p(95)<200ms: ${passed}\n`;
  }

  const failedThreshold = data.metrics.http_req_failed?.thresholds || {};
  const errorThreshold = failedThreshold['rate<0.01'];
  if (errorThreshold) {
    const passed = errorThreshold.ok ? colorize('PASS', 'green') : colorize('FAIL', 'red');
    summary += `    http_req_failed rate<1%:       ${passed}\n`;
  }

  summary += '\n';

  return summary;
}
