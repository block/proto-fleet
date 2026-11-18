import { sha256 } from "@noble/hashes/sha2.js";
import { bytesToHex } from "@noble/hashes/utils.js";

export async function sha256Hex(bytes: Uint8Array<ArrayBuffer>): Promise<string> {
  // Fleet supports HTTP deployments, where WebCrypto's digest API is unavailable.
  const digest = globalThis.crypto?.subtle
    ? new Uint8Array(await crypto.subtle.digest("SHA-256", bytes))
    : sha256(bytes);
  return bytesToHex(digest);
}

export async function computeSha256(file: Blob): Promise<string> {
  return sha256Hex(new Uint8Array(await file.arrayBuffer()));
}

export const createOperationID = () => {
  // getRandomValues is available in the plain-HTTP deployments Fleet supports,
  // while crypto.randomUUID() is restricted to secure contexts.
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
};
