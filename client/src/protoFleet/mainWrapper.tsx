import ReactDOM from "react-dom/client";

import Main from "./main";
import { API_PROXY_BASE } from "@/protoFleet/api/constants";
import { getConfigValue, initObservability } from "@/shared/observability";
// Side effect: registers observability providers before init runs.
import "@/shared/observability/providers";
import { buildVersionInfo, logBuildVersion } from "@/shared/utils/version";

logBuildVersion();

initObservability({
  service: "proto-fleet-client",
  version: buildVersionInfo.commit,
  env: getConfigValue("DD_ENV") ?? (import.meta.env.PROD ? "production" : "development"),
  apiTracingOrigin: API_PROXY_BASE,
});

ReactDOM.createRoot(document.getElementById("root")!).render(<Main />);
