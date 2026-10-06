/**
 * Runtime-first configuration accessor for the observability layer.
 *
 * Resolution order for a key:
 *   1. `window.__RUNTIME_CONFIG__[key]` — rendered by the deployment nginx image
 *      at container start from operator-supplied environment variables. This lets
 *      an operator enable a provider on a prebuilt client artifact without a rebuild.
 *   2. `import.meta.env.VITE_<key>` — the build-time value used by local dev and CI.
 *
 * An empty (or whitespace-only) value is treated as unset so a rendered-but-blank
 * runtime slot falls through to the build-time value (or `undefined`). Providers use
 * this to stay a complete no-op when their required keys are absent.
 */

declare global {
  interface Window {
    __RUNTIME_CONFIG__?: Record<string, string>;
  }
}

const normalize = (value: string | undefined): string | undefined => {
  if (value === undefined) {
    return undefined;
  }
  const trimmed = value.trim();
  return trimmed === "" ? undefined : trimmed;
};

export const getConfigValue = (key: string): string | undefined => {
  const runtime = typeof window !== "undefined" ? window.__RUNTIME_CONFIG__?.[key] : undefined;
  const runtimeValue = normalize(runtime);
  if (runtimeValue !== undefined) {
    return runtimeValue;
  }

  const env = import.meta.env as Record<string, string | undefined>;
  return normalize(env[`VITE_${key}`]);
};
