/** Pluggable client observability. To add a backend, see `providers.ts`. */

export type { ObservabilityProvider, ObservabilityInitContext, ObservabilityErrorMeta } from "./types";
export { registerProvider, initObservability, reportObservabilityError, observabilityInterceptors } from "./registry";
