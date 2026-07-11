import type { Interceptor } from "@connectrpc/connect";

import type { ObservabilityErrorMeta, ObservabilityInitContext, ObservabilityProvider } from "./types";

/**
 * Vendor-neutral observability registry.
 *
 * Providers register themselves (see `providers.ts`, imported for its side effect
 * at app startup). Adding a new backend means writing one provider module and
 * registering it here — no changes to the entry point, transport, or error boundary.
 */

const providers: ObservabilityProvider[] = [];
let initialized = false;

/** Register a provider. Idempotent by `name`. */
export const registerProvider = (provider: ObservabilityProvider): void => {
  if (!providers.some((existing) => existing.name === provider.name)) {
    providers.push(provider);
  }
};

/** Initialize every configured provider exactly once. A provider failure is
 * isolated so it can never block app startup. */
export const initObservability = (context: ObservabilityInitContext): void => {
  if (initialized) {
    return;
  }
  initialized = true;

  for (const provider of providers) {
    if (!provider.isConfigured()) {
      continue;
    }
    try {
      provider.init(context);
    } catch (error) {
      console.error(`[observability] provider "${provider.name}" failed to initialize`, error);
    }
  }
};

/** Forward a captured error to every configured provider. Never throws. */
export const reportObservabilityError = (error: unknown, meta?: ObservabilityErrorMeta): void => {
  for (const provider of providers) {
    if (!provider.isConfigured() || !provider.reportError) {
      continue;
    }
    try {
      provider.reportError(error, meta);
    } catch {
      // Error reporting must never surface its own failure.
    }
  }
};

/** Collect ConnectRPC interceptors contributed by configured providers. */
export const observabilityInterceptors = (): Interceptor[] => {
  const interceptors: Interceptor[] = [];
  for (const provider of providers) {
    if (provider.isConfigured() && provider.connectInterceptors) {
      interceptors.push(...provider.connectInterceptors());
    }
  }
  return interceptors;
};

/** Reset registry state. Test-only. */
export const __resetObservabilityForTests = (): void => {
  providers.length = 0;
  initialized = false;
};
