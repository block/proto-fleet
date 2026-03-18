import { createConnectTransport } from "@connectrpc/connect-web";
import { API_PROXY_BASE } from "@/protoFleet/api/constants";

const transport = createConnectTransport({
  baseUrl: `${API_PROXY_BASE}/`,
  // Include cookies with all requests for session-based authentication
  fetch: (input, init) => fetch(input, { ...init, credentials: "include" }),
});

export { transport };
