import { type LoaderFunctionArgs } from "react-router-dom";
import { beforeEach, describe, expect, test } from "vitest";

import { minersRedirectLoader, racksRedirectLoader, sitesRedirectLoader } from "./redirectLoaders";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

// Invoke a react-router loader with a stub LoaderFunctionArgs containing
// only the fields the redirect loader actually uses. Cast keeps the test
// focused on the search + hash contract instead of building a full
// DataRouterArgs (LoaderFunctionArgs in newer react-router versions adds
// `url` and `pattern` that we don't exercise here).
const invoke = async (loader: typeof minersRedirectLoader, url: string): Promise<Response> => {
  const request = new Request(url);
  const args = { request, params: {} } as unknown as LoaderFunctionArgs;
  const result = await loader(args);
  if (!(result instanceof Response)) {
    throw new Error("Redirect loader did not return a Response");
  }
  return result;
};

const setActiveSite = (activeSite = DEFAULT_ACTIVE_SITE) => {
  useFleetStore.setState((state) => {
    state.ui.activeSite = activeSite;
  });
};

describe("redirectLoaders", () => {
  beforeEach(() => {
    setActiveSite();
  });

  describe("minersRedirectLoader", () => {
    test("redirects /miners to /fleet/miners with no query string", async () => {
      const response = await invoke(minersRedirectLoader, "http://localhost/miners");
      expect(response.status).toBe(302);
      expect(response.headers.get("Location")).toBe("/fleet/miners");
    });

    test("preserves the search string (control-board filter deep-link)", async () => {
      const response = await invoke(minersRedirectLoader, "http://localhost/miners?filter=control-board-issue");
      expect(response.headers.get("Location")).toBe("/fleet/miners?filter=control-board-issue");
    });

    test("preserves multi-param search + hash", async () => {
      const response = await invoke(minersRedirectLoader, "http://localhost/miners?filter=fans&duration=24h#section-a");
      expect(response.headers.get("Location")).toBe("/fleet/miners?filter=fans&duration=24h#section-a");
    });

    test("preserves the stored site scope", async () => {
      setActiveSite({ kind: "site", id: "7" });
      const response = await invoke(minersRedirectLoader, "http://localhost/miners?filter=fans");
      expect(response.headers.get("Location")).toBe("/7/fleet/miners?filter=fans");
    });

    test("keeps explicit site filters in all-sites scope", async () => {
      setActiveSite({ kind: "site", id: "7" });
      const response = await invoke(minersRedirectLoader, "http://localhost/miners?site=8&filter=fans");
      expect(response.headers.get("Location")).toBe("/fleet/miners?site=8&filter=fans");
    });

    test("preserves stored scope when site filter params are malformed", async () => {
      setActiveSite({ kind: "site", id: "7" });
      const response = await invoke(minersRedirectLoader, "http://localhost/miners?site=abc&filter=fans");
      expect(response.headers.get("Location")).toBe("/7/fleet/miners?site=abc&filter=fans");
    });
  });

  describe("racksRedirectLoader", () => {
    test("redirects /racks to /fleet/racks with no query string", async () => {
      const response = await invoke(racksRedirectLoader, "http://localhost/racks");
      expect(response.headers.get("Location")).toBe("/fleet/racks");
    });

    test("preserves the rack filter deep-link", async () => {
      const response = await invoke(racksRedirectLoader, "http://localhost/racks?rack=A-01");
      expect(response.headers.get("Location")).toBe("/fleet/racks?rack=A-01");
    });

    test("preserves search and hash together", async () => {
      const response = await invoke(racksRedirectLoader, "http://localhost/racks?building=42#perf");
      expect(response.headers.get("Location")).toBe("/fleet/racks?building=42#perf");
    });

    test("keeps explicit site filters in all-sites scope", async () => {
      setActiveSite({ kind: "site", id: "7" });
      const response = await invoke(racksRedirectLoader, "http://localhost/racks?site=8&rack=A-01");
      expect(response.headers.get("Location")).toBe("/fleet/racks?site=8&rack=A-01");
    });

    test("recognizes comma-separated explicit site filters", async () => {
      setActiveSite({ kind: "site", id: "7" });
      const response = await invoke(racksRedirectLoader, "http://localhost/racks?site=abc,8&rack=A-01");
      expect(response.headers.get("Location")).toBe("/fleet/racks?site=abc,8&rack=A-01");
    });
  });

  describe("sitesRedirectLoader", () => {
    test("redirects /sites to /fleet/sites with no query string", async () => {
      const response = await invoke(sitesRedirectLoader, "http://localhost/sites");
      expect(response.status).toBe(302);
      expect(response.headers.get("Location")).toBe("/fleet/sites");
    });

    test("preserves search and hash", async () => {
      const response = await invoke(sitesRedirectLoader, "http://localhost/sites?view=grid#summary");
      expect(response.headers.get("Location")).toBe("/fleet/sites?view=grid#summary");
    });
  });
});
