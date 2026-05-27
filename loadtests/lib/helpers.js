// UUID v6 generation (lexicographically sortable, k6-compatible)
// Based on UUID v1 timestamp structure but reordered for sortability

export function generateUUIDv6() {
  // Get current timestamp in 100-nanosecond intervals since Oct 15, 1582
  const now = Date.now();
  const unixTimestamp = now * 10000 + 122192928000000000;

  // Split into time components
  const timeLow = unixTimestamp & 0xffffffff;
  const timeMid = (unixTimestamp >> 32) & 0xffff;
  const timeHiAndVersion = ((unixTimestamp >> 48) & 0x0fff) | 0x6000; // Version 6

  // Generate random clock sequence and node
  const clockSeq = Math.floor(Math.random() * 0x3fff) | 0x8000;
  const node = new Uint8Array(6);
  for (let i = 0; i < 6; i++) {
    node[i] = Math.floor(Math.random() * 256);
  }

  // Format as UUID string
  const hex = (n, len) => n.toString(16).padStart(len, '0');
  return `${hex(timeHiAndVersion, 4)}${hex(timeMid, 4)}-${hex(timeLow >> 16, 4)}-${hex(timeLow & 0xffff, 4)}-${hex(clockSeq, 4)}-${hex(node[0], 2)}${hex(node[1], 2)}${hex(node[2], 2)}${hex(node[3], 2)}${hex(node[4], 2)}${hex(node[5], 2)}`;
}

// Generate idempotency key for payout requests
export function generateIdempotencyKey() {
  return generateUUIDv6();
}

// Create payout request
export function createPayout(baseUrl, payload) {
  const url = `${baseUrl}/v1/payouts`;
  const idempotencyKey = generateIdempotencyKey();

  const params = {
    headers: {
      'Content-Type': 'application/json',
      'Idempotency-Key': idempotencyKey,
    },
  };

  return http.post(url, JSON.stringify(payload), params);
}

// Get payout by ID
export function getPayout(baseUrl, payoutId) {
  const url = `${baseUrl}/v1/payouts/${payoutId}`;

  const params = {
    headers: {
      'Content-Type': 'application/json',
    },
  };

  return http.get(url, params);
}

// Cancel payout
export function cancelPayout(baseUrl, payoutId) {
  const url = `${baseUrl}/v1/payouts/${payoutId}/cancel`;

  const params = {
    headers: {
      'Content-Type': 'application/json',
    },
  };

  return http.post(url, null, params);
}

// Generate random payout payload
export function randomPayoutPayload(rail = 'card') {
  const currencies = ['USD', 'EUR', 'GBP'];
  const currency = currencies[Math.floor(Math.random() * currencies.length)];
  const amount = Math.floor(Math.random() * 10000) + 100; // 100 - 10100 cents

  return {
    amount: amount,
    currency: currency,
    rail: rail,
  };
}

// Health check
export function healthCheck(baseUrl) {
  return http.get(`${baseUrl}/health`);
}
