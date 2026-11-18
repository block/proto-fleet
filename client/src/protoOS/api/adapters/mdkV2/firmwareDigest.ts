// FIPS 180-4 SHA-256 for image integrity (not signature verification). CT1 is
// served over LAN HTTP, where SubtleCrypto is unavailable. The hash is fed in
// pieces, so a large file is never held in memory to be hashed.
const constants = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5, 0xd807aa98,
  0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786,
  0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da, 0x983e5152, 0xa831c66d, 0xb00327c8,
  0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
  0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85, 0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819,
  0xd6990624, 0xf40e3585, 0x106aa070, 0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a,
  0x5b9cca4f, 0x682e6ff3, 0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7,
  0xc67178f2,
]);
const rotate = (n: number, bits: number) => (n >>> bits) | (n << (32 - bits));

export interface Sha256 {
  update(bytes: Uint8Array): void;
  /** Lowercase hex. The hash can't be fed after this. */
  digest(): string;
}

export function createSha256(): Sha256 {
  const hash = new Uint32Array([
    0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19,
  ]);
  const words = new Uint32Array(64);
  // Bytes that don't fill a 64-byte block yet.
  const pending = new Uint8Array(64);
  let filled = 0;
  let length = 0;

  const compress = (bytes: Uint8Array, at: number) => {
    for (let i = 0; i < 16; i++) {
      const j = at + i * 4;
      words[i] = (bytes[j] << 24) | (bytes[j + 1] << 16) | (bytes[j + 2] << 8) | bytes[j + 3];
    }
    for (let i = 16; i < 64; i++) {
      const a = words[i - 15],
        b = words[i - 2];
      words[i] =
        words[i - 16] +
        (rotate(a, 7) ^ rotate(a, 18) ^ (a >>> 3)) +
        words[i - 7] +
        (rotate(b, 17) ^ rotate(b, 19) ^ (b >>> 10));
    }
    let [a, b, c, d, e, f, g, h] = hash;
    for (let i = 0; i < 64; i++) {
      const t1 =
        (h + (rotate(e, 6) ^ rotate(e, 11) ^ rotate(e, 25)) + ((e & f) ^ (~e & g)) + constants[i] + words[i]) | 0;
      const t2 = ((rotate(a, 2) ^ rotate(a, 13) ^ rotate(a, 22)) + ((a & b) ^ (a & c) ^ (b & c))) | 0;
      h = g;
      g = f;
      f = e;
      e = (d + t1) | 0;
      d = c;
      c = b;
      b = a;
      a = (t1 + t2) | 0;
    }
    [a, b, c, d, e, f, g, h].forEach((value, i) => {
      hash[i] += value;
    });
  };

  return {
    update(bytes) {
      length += bytes.length;
      let at = 0;
      if (filled) {
        at = Math.min(64 - filled, bytes.length);
        pending.set(bytes.subarray(0, at), filled);
        filled += at;
        if (filled < 64) return;
        compress(pending, 0);
        filled = 0;
      }
      for (; at + 64 <= bytes.length; at += 64) compress(bytes, at);
      pending.set(bytes.subarray(at));
      filled = bytes.length - at;
    },
    digest() {
      const tail = new Uint8Array(filled < 56 ? 64 : 128);
      tail.set(pending.subarray(0, filled));
      tail[filled] = 0x80;
      const view = new DataView(tail.buffer);
      const bitLength = length * 8;
      view.setUint32(tail.length - 8, Math.floor(bitLength / 0x100000000));
      view.setUint32(tail.length - 4, bitLength >>> 0);
      for (let at = 0; at < tail.length; at += 64) compress(tail, at);
      return Array.from(hash, (n) => n.toString(16).padStart(8, "0")).join("");
    },
  };
}

// Bytes hashed between yields to the event loop, so the page stays responsive and the request can be stopped.
const SLICE_BYTES = 4096 * 64;

export async function firmwareDigest(bytes: Uint8Array, signal?: AbortSignal | null): Promise<string> {
  const sha256 = createSha256();
  for (let at = 0; at === 0 || at < bytes.length; at += SLICE_BYTES) {
    signal?.throwIfAborted();
    await new Promise<void>((resolve) => setTimeout(resolve, 0));
    sha256.update(bytes.subarray(at, at + SLICE_BYTES));
  }
  signal?.throwIfAborted();
  return sha256.digest();
}
