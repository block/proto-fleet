import { createConnectTransport } from "@connectrpc/connect-web";

// If vite is serving the app one of these will be true
const isLocal = import.meta.env.DEV || import.meta.env.PROD;

// This transport is going to be used throughout the app
// For local development we use the relative path and proxy requests to the backend
// using vites proxy configuration. This prevents CORS issues.
// TODO read this value from build config
const transport = createConnectTransport({
  baseUrl: isLocal ? "/" : "backend.develop.fleetdev.proto.xyz",
});

export { transport };
