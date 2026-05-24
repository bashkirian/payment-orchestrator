import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter } from 'k6/metrics';

// Custom metrics
const requestsAllowed = new Counter('requests_allowed');
const requestsRejected = new Counter('requests_rejected');
const recoverySuccess = new Counter('recovery_success');

// Test configuration
export const options = {
  stages: [
    { duration: '5s', target: 50 },    // Quick ramp up
    { duration: '10s', target: 50 },   // Burst phase - should trigger rate limits
    { duration: '5s', target: 0 },     // Ramp down
    { duration: '15s', target: 0 },    // Wait for token refill
    { duration: '5s', target: 20 },    // Recovery test
    { duration: '10s', target: 0 },    // Final ramp down
  ],
  thresholds: {
    http_req_failed: ['rate<0.5'],     // Allow high error rate during burst
  },
};

const BASE_URL = __ENV.API_URL || 'http://localhost:8080';

export default function () {
  // Use same idempotency key to test rate limiting without creating duplicates
  const idempotencyKey = `stress-test-${__VU}-${__ITER}`;
  const payload = {
    amount: 1000,
    currency: 'USD',
    rail: 'card',
  };

  const params = {
    headers: {
      'Content-Type': 'application/json',
      'Idempotency-Key': idempotencyKey,
    },
  };

  const response = http.post(`${BASE_URL}/v1/payouts`, JSON.stringify(payload), params);

  // Track rate limiting behavior
  if (response.status === 202) {
    requestsAllowed.add(1);
  } else if (response.status === 429) {
    requestsRejected.add(1);
    check(response, {
      'has retry-after header': (r) => r.headers['Retry-After'] !== undefined,
      'has error message': (r) => {
        try {
          const body = JSON.parse(r.body);
          return body.error === 'rate limit exceeded';
        } catch {
          return false;
        }
      },
    });
  }

  // No sleep - burst requests to trigger rate limiting
}

export function handleSummary(data) {
  return {
    stdout: textSummary(data, { indent: ' ', enableColors: true }),
    'loadtests/results/rate-limit-stress-summary.json': JSON.stringify(data, null, 2),
  };
}

function textSummary(data, options = {}) {
  const colors = options.enableColors || false;
  const colorize = (str, color) => colors ? str : str;

  let summary = '\n' + colorize('=', 'cyan') + '\n';
  summary += colorize('  RATE LIMIT STRESS TEST SUMMARY', 'cyan') + '\n';
  summary += colorize('=', 'cyan') + '\n\n';

  // HTTP metrics
  const httpReqs = data.metrics.http_reqs?.values?.count || 0;

  summary += `  Total Requests:     ${httpReqs}\n\n`;

  // Rate limiting metrics
  const allowed = data.metrics.requests_allowed?.values?.count || 0;
  const rejected = data.metrics.requests_rejected?.values?.count || 0;
  const total = allowed + rejected;
  const rejectionRate = total > 0 ? (rejected / total * 100) : 0;

  summary += `  Requests Allowed:   ${allowed}\n`;
  summary += `  Requests Rejected:  ${rejected}\n`;
  summary += `  Rejection Rate:     ${rejectionRate.toFixed(2)}%\n\n`;

  // Check if rate limiter is working
  if (rejected > 0) {
    summary += colorize('  ✓ Rate limiter is active and rejecting requests', 'green') + '\n';
  } else {
    summary += colorize('  ⚠ No rate limiting detected - check configuration', 'yellow') + '\n';
  }

  summary += '\n';

  return summary;
}
