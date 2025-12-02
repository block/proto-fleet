import { createConnectTransport } from "@connectrpc/connect-web";

const transport = createConnectTransport({
  baseUrl: "/api-proxy/",
  // Include cookies with all requests for session-based authentication
  fetch: (input, init) => fetch(input, { ...init, credentials: "include" }),
});

export { transport };
