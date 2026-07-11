/**
 * Pluggable client observability.
 *
 * To add a backend (e.g. Sentry, PostHog): create a module under `providers/`
 * implementing `ObservabilityProvider`, then register it in `providers.ts`.
 * Nothing else — entry point, transport, and error boundary are provider-agnostic.
 */

export type { ObservabilityProvider, ObservabilityInitContext, ObservabilityErrorMeta } from "./types";
export { getConfigValue } from "./runtimeConfig";
export { registerProvider, initObservability, reportObservabilityError, observabilityInterceptors } from "./registry";
