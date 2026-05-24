import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Trend } from 'k6/metrics';
import { generateIdempotencyKey, randomPayoutPayload } from '../lib/helpers.js';

// Custom metrics
const fullFlowCompleted = new Counter('full_flow_completed');
const fullFlowFailed = new Counter('full_flow_failed');
const flowLatency = new Trend('flow_latency');

// Test configuration
export const options = {
  stages: [
    { duration: '30s', target: 30 },   // Ramp up to 30 VUs
    { duration: '2m', target: 30 },    // Stay at 30 VUs
    { duration: '30s', target: 0 },    // Ramp down
  ],
  thresholds: {
    http_req_duration: ['p(95)<300'],  // 95% of requests < 300ms (full flow)
    http_req_failed: ['rate<0.01'],    // Error rate < 1%
  },
};

const BASE_URL = __ENV.API_URL || 'http://localhost:8080';

export default function () {
  const flowStart = new Date();
  let flowSuccess = true;

  // Step 1: Create payout
  const idempotencyKey = generateIdempotencyKey();
  const payload = randomPayoutPayload('card');

  const createParams = {
    headers: {
      'Content-Type': 'application/json',
      'Idempotency-Key': idempotencyKey,
    },
  };

  const createResponse = http.post(`${BASE_URL}/v1/payouts`, JSON.stringify(payload), createParams);

  const createSuccess = check(createResponse, {
    'create: status is 202': (r) => r.status === 202,
    'create: has payout_id': (r) => {
      try {
        const body = JSON.parse(r.body);
        return body.payout_id !== undefined;
      } catch {
        return false;
      }
    },
  });

  if (!createSuccess) {
    flowSuccess = false;
    fullFlowFailed.add(1);
    return;
  }

  // Extract payout_id
  let payoutId;
  try {
    payoutId = JSON.parse(createResponse.body).payout_id;
  } catch {
    flowSuccess = false;
    fullFlowFailed.add(1);
    return;
  }

  // Step 2: Get payout (verify creation)
  const getParams = {
    headers: {
      'Content-Type': 'application/json',
    },
  };

  const getResponse = http.get(`${BASE_URL}/v1/payouts/${payoutId}`, getParams);

  const getSuccess = check(getResponse, {
    'get: status is 200': (r) => r.status === 200,
    'get: correct payout_id': (r) => {
      try {
        const body = JSON.parse(r.body);
        return body.payout_id === payoutId;
      } catch {
        return false;
      }
    },
  });

  if (!getSuccess) {
    flowSuccess = false;
    fullFlowFailed.add(1);
    return;
  }

  // Step 3: Idempotency check - retry with same key
  const retryResponse = http.post(`${BASE_URL}/v1/payouts`, JSON.stringify(payload), createParams);

  check(retryResponse, {
    'idempotency: same payout_id': (r) => {
      try {
        const body = JSON.parse(r.body);
        return body.payout_id === payoutId;
      } catch {
        return false;
      }
    },
  });

  // Record flow completion
  const flowDuration = new Date() - flowStart;
  flowLatency.add(flowDuration);

  if (flowSuccess) {
    fullFlowCompleted.add(1);
  }

  // Think time between flows
  sleep(0.5);
}

export function handleSummary(data) {
  return {
    stdout: textSummary(data, { indent: ' ', enableColors: true }),
    'loadtests/results/full-flow-summary.json': JSON.stringify(data, null, 2),
  };
}

function textSummary(data, options = {}) {
  const indent = options.indent || '  ';
  const colors = options.enableColors || false;

  const colorize = (str, color) => colors ? str : str;

  let summary = '\n' + colorize('=', 'cyan') + '\n';
  summary += colorize('  FULL FLOW LOAD TEST SUMMARY', 'cyan') + '\n';
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
  const completed = data.metrics.full_flow_completed?.values?.count || 0;
  const failed = data.metrics.full_flow_failed?.values?.count || 0;

  summary += `  Flows Completed:    ${completed}\n`;
  summary += `  Flows Failed:       ${failed}\n\n`;

  // Flow latency
  const flowLat = data.metrics.flow_latency?.values || {};
  summary += `  Flow Latency (p95): ${(flowLat['p(95)'] || 0).toFixed(2)}ms\n\n`;

  // Thresholds
  summary += colorize('  THRESHOLDS', 'yellow') + '\n';
  const thresholds = data.metrics.http_req_duration?.thresholds || {};
  const durationThreshold = thresholds['p(95)<300'];
  if (durationThreshold) {
    const passed = durationThreshold.ok ? colorize('PASS', 'green') : colorize('FAIL', 'red');
    summary += `    http_req_duration p(95)<300ms: ${passed}\n`;
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
