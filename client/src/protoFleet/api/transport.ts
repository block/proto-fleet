import type { Transport } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";

import { API_PROXY_BASE } from "@/protoFleet/api/constants";

let transport: Transport;

if (import.meta.env.VITE_MOCK_DATA === "true") {
  const { mockTransport } = await import("@/protoFleet/mocks/mockTransport");
  transport = mockTransport;
} else {
  transport = createConnectTransport({
    baseUrl: `${API_PROXY_BASE}/`,
    fetch: (input, init) => fetch(input, { ...init, credentials: "include" }),
  });
}

export { transport };
