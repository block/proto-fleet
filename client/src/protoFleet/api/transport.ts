import { createConnectTransport } from "@connectrpc/connect-web";

// This transport is going to be used throughout the app
// TODO read this value from build config
const transport = createConnectTransport({
  baseUrl:
    import.meta.env.VITE_CONNECT_TRANSPORT_URL ||
    "backend.develop.fleetdev.proto.xyz",
});

export { transport };
