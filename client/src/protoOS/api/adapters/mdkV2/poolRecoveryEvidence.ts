import type { MiningGroupPoolSettings } from "../poolConfiguration";
import { createSha256 } from "./firmwareDigest";
import { publicPools } from "./poolEdits";

export const poolRecoveryFingerprint = (pools: MiningGroupPoolSettings[]) => {
  const digest = createSha256();
  digest.update(new TextEncoder().encode(JSON.stringify(publicPools(pools))));
  return digest.digest();
};

export const recoveryPoolURL = (value: string) => {
  const shorthand = !value.includes("://");
  try {
    const url = new URL(shorthand ? `stratum+tcp://${value}` : value);
    if (!url.hostname) return "";
    if (url.username || url.password || /[?#]/.test(value) || !["", "/"].includes(url.pathname))
      return shorthand ? url.host : `${url.protocol}//${url.host}`;
    return value;
  } catch {
    return "";
  }
};
