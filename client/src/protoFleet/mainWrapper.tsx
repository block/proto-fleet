import ReactDOM from "react-dom/client";

import Main from "./main";
import { API_PROXY_BASE } from "@/protoFleet/api/constants";
import { initObservability } from "@/shared/observability";
// Side effect: registers observability providers before init runs.
import "@/shared/observability/providers";
import { buildVersionInfo, logBuildVersion } from "@/shared/utils/version";

logBuildVersion();

// Provider-agnostic context: each provider layers its own config keys on top
// (e.g. the Datadog provider prefers DD_ENV over this default).
initObservability({
  service: "proto-fleet-client",
  version: buildVersionInfo.commit,
  env: import.meta.env.PROD ? "production" : "development",
  apiTracingPathPrefix: API_PROXY_BASE,
});

ReactDOM.createRoot(document.getElementById("root")!).render(<Main />);
