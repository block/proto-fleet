import type { Interceptor } from "@connectrpc/connect";

/** Context passed to every provider at startup. App-specific values so `shared/`
 * stays free of any dependency on a specific app (`protoFleet` / `protoOS`). */
export interface ObservabilityInitContext {
  /** Logical service name reported to the backend (e.g. "proto-fleet-client"). */
  service: string;
  /** Build identifier (commit or version) for correlating sessions to a release. */
  version: string;
  /** Deploy environment (e.g. "production", "staging", "dev"). */
  env: string;
  /** Origin or path prefix of client→server API calls, used to scope distributed tracing. */
  apiTracingOrigin: string;
}

export type ObservabilityErrorMeta = Record<string, unknown>;

/**
 * A pluggable observability backend (Datadog, and later Sentry/PostHog).
 *
 * A provider is self-describing: it reads its own configuration and reports
 * whether it `isConfigured()`. The registry only initializes configured providers,
 * so an unconfigured provider is a complete no-op.
 */
export interface ObservabilityProvider {
  /** Stable identifier, used in logs and to de-duplicate registration. */
  readonly name: string;
  /** True only when this provider's required configuration is present. */
  isConfigured(): boolean;
  /** Initialize the provider. Called at most once by the registry. */
  init(context: ObservabilityInitContext): void;
  /** Forward a captured error to the provider. Optional. */
  reportError?(error: unknown, meta?: ObservabilityErrorMeta): void;
  /** ConnectRPC interceptors this provider contributes for RPC instrumentation. Optional. */
  connectInterceptors?(): Interceptor[];
}
