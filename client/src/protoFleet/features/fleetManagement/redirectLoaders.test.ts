import { type LoaderFunctionArgs } from "react-router-dom";
import { describe, expect, test } from "vitest";

import { minersRedirectLoader, racksRedirectLoader, sitesRedirectLoader } from "./redirectLoaders";

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

describe("redirectLoaders", () => {
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
  });

  describe("sitesRedirectLoader", () => {
    test("redirects /sites to /fleet/sites with no query string", async () => {
      const response = await invoke(sitesRedirectLoader, "http://localhost/sites");
      expect(response.status).toBe(302);
      expect(response.headers.get("Location")).toBe("/fleet/sites");
    });

    test("redirects /settings/sites to /fleet/sites with no query string", async () => {
      // Same loader is mounted at /settings/sites; the destination is the
      // operator-facing Sites tab regardless of which legacy URL they hit.
      const response = await invoke(sitesRedirectLoader, "http://localhost/settings/sites");
      expect(response.headers.get("Location")).toBe("/fleet/sites");
    });

    test("preserves search and hash", async () => {
      const response = await invoke(sitesRedirectLoader, "http://localhost/sites?view=grid#summary");
      expect(response.headers.get("Location")).toBe("/fleet/sites?view=grid#summary");
    });
  });
});
