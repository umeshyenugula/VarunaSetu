// Browser Ed25519 signing for HydroLedger simulator demo.
// Must match hydroledger/verifier/identity/keys.go signingBytes exactly.

const DOMAIN = 'hydroledger/identity/telemetry/v1';

function u32be(n: number): Uint8Array {
  const b = new Uint8Array(4);
  new DataView(b.buffer).setUint32(0, n >>> 0, false);
  return b;
}

function i64be(seconds: number): Uint8Array {
  // seconds within safe integer range for demo timestamps
  const b = new Uint8Array(8);
  const hi = Math.floor(seconds / 0x100000000);
  const lo = seconds >>> 0;
  new DataView(b.buffer).setUint32(0, hi >>> 0, false);
  new DataView(b.buffer).setUint32(4, lo, false);
  return b;
}

function concat(parts: Uint8Array[]): Uint8Array {
  const total = parts.reduce((n, p) => n + p.length, 0);
  const out = new Uint8Array(total);
  let o = 0;
  for (const p of parts) { out.set(p, o); o += p.length; }
  return out;
}

function appendString(s: string): Uint8Array {
  const bytes = new TextEncoder().encode(s);
  return concat([u32be(bytes.length), bytes]);
}

function appendBytes(bytes: Uint8Array): Uint8Array {
  return concat([u32be(bytes.length), bytes]);
}

export function canonicalSigningBytes(msg: {
  deviceId: string;
  timestamp: Date;
  nonce: string;
  payloadBytes: Uint8Array;
}): Uint8Array {
  const ms = msg.timestamp.getTime();
  const sec = Math.floor(ms / 1000);
  const nsec = (ms % 1000) * 1000000;
  const domainBytes = new TextEncoder().encode(DOMAIN);
  return concat([
    domainBytes,
    new Uint8Array([0]),
    appendString(msg.deviceId),
    i64be(sec),
    u32be(nsec),
    appendString(msg.nonce),
    appendBytes(msg.payloadBytes),
  ]);
}

// PKCS8 wrapper for Ed25519 seed (RFC 8410): 302e020100300506032b657004220420 + 32-byte seed
function seedToPkcs8(seed: Uint8Array): Uint8Array {
  if (seed.length !== 32) throw new Error('Ed25519 seed must be 32 bytes');
  const prefix = Uint8Array.from([48, 46, 2, 1, 0, 48, 5, 6, 3, 43, 101, 112, 4, 34, 4, 32]);
  return concat([prefix, seed]);
}

export async function importPrivateKey(seedBase64: string): Promise<CryptoKey> {
  const seed = Uint8Array.from(atob(seedBase64), c => c.charCodeAt(0));
  const pkcs8 = seedToPkcs8(seed);
  return crypto.subtle.importKey('pkcs8', pkcs8 as BufferSource, { name: 'Ed25519' }, false, ['sign']);
}

export function base64RawStd(bytes: ArrayBuffer | Uint8Array): string {
  const u8 = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes);
  let bin = '';
  for (let i = 0; i < u8.length; i++) bin += String.fromCharCode(u8[i]);
  return btoa(bin).replace(/=+$/g, ''); // RawStdEncoding = no padding
}

export function randomNonce(): string {
  const b = new Uint8Array(16);
  crypto.getRandomValues(b);
  // base64url no padding, matches Go NewNonce style (server accepts any nonce)
  return base64RawStd(b).replace(/\+/g, '-').replace(/\//g, '_');
}

export interface SignedTelemetryEnvelope {
  device_id: string;
  timestamp: string; // RFC3339
  nonce: string;
  payload: unknown;
  signature: string; // base64 RawStdEncoding
}

export async function signTelemetry(args: {
  deviceId: string;
  privateSeedBase64: string;
  timestamp: Date;
  nonce: string;
  payload: Record<string, unknown>;
}): Promise<SignedTelemetryEnvelope> {
  // Payload bytes must be the exact JSON that the server decodes/verifies.
  const payloadJson = JSON.stringify(args.payload);
  const payloadBytes = new TextEncoder().encode(payloadJson);
  const signingBytes = canonicalSigningBytes({
    deviceId: args.deviceId,
    timestamp: args.timestamp,
    nonce: args.nonce,
    payloadBytes,
  });
  const key = await importPrivateKey(args.privateSeedBase64);
  const sig = await crypto.subtle.sign('Ed25519', key, signingBytes as BufferSource);
  return {
    device_id: args.deviceId,
    timestamp: args.timestamp.toISOString(),
    nonce: args.nonce,
    payload: JSON.parse(payloadJson),
    signature: base64RawStd(sig),
  };
}
