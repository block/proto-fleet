import { createConnectTransport } from "@connectrpc/connect-web";
import { API_PROXY_BASE } from "@/protoFleet/api/constants";
import { observabilityInterceptors } from "@/shared/observability";
// Side effect: registers observability providers before interceptors are collected.
import "@/shared/observability/providers";

const transport = createConnectTransport({
  baseUrl: `${API_PROXY_BASE}/`,
  // Include cookies with all requests for session-based authentication
  fetch: (input, init) => fetch(input, { ...init, credentials: "include" }),
  // Observability providers may contribute RPC instrumentation. Empty when none
  // are configured, so transport behavior is unchanged in that case.
  interceptors: observabilityInterceptors(),
});

export { transport };
