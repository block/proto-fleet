import { datadogProvider } from "./providers/datadog";
import { registerProvider } from "./registry";

/**
 * Registers the built-in observability providers as a side effect of import.
 *
 * Imported by both the app entry point (before `initObservability`) and the API
 * transport (before it collects interceptors), so registration always precedes
 * use regardless of module load order. Registration is idempotent by provider name.
 *
 * To add a provider (Sentry, PostHog): implement it under `providers/` and add a
 * `registerProvider(...)` call here — no other file needs to change.
 */
registerProvider(datadogProvider);
