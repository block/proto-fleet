/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Datadog RUM application ID. Required (with the client token) to enable RUM. */
  readonly VITE_DD_APPLICATION_ID?: string;
  /** Datadog RUM client token. Required (with the application ID) to enable RUM. */
  readonly VITE_DD_CLIENT_TOKEN?: string;
  /** Datadog site (e.g. "datadoghq.com", "us3.datadoghq.com"). Defaults to datadoghq.com. */
  readonly VITE_DD_SITE?: string;
  /** Service name reported to Datadog. Defaults to the app's init-context service. */
  readonly VITE_DD_SERVICE?: string;
  /** Deploy environment reported to Datadog. Defaults to the app's init-context env. */
  readonly VITE_DD_ENV?: string;
  /** RUM session sample rate, 0-100. Defaults to 100. */
  readonly VITE_DD_RUM_SAMPLE_RATE?: string;
  /** Session Replay sample rate, 0-100. Defaults to 0 (off). */
  readonly VITE_DD_SESSION_REPLAY_SAMPLE_RATE?: string;
  /** Distributed-tracing sample rate for API calls, 0-100. Defaults to 100. */
  readonly VITE_DD_TRACE_SAMPLE_RATE?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
