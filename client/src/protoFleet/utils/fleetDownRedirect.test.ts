import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { redirectFromFleetDown } from "./fleetDownRedirect";

describe("redirectFromFleetDown", () => {
  const mockLocation = {
    href: "",
    search: "",
    pathname: "/fleet-down",
    hash: "",
    host: "localhost:5173",
    hostname: "localhost",
    origin: "http://localhost:5173",
    port: "5173",
    protocol: "http:",
  };

  beforeEach(() => {
    // Mock window.location properties
    Object.defineProperty(window, "location", {
      value: mockLocation,
      writable: true,
      configurable: true,
    });
    mockLocation.href = "";
    mockLocation.search = "";
  });

  afterEach(() => {
    // Clean up
    mockLocation.href = "";
    mockLocation.search = "";
  });

  it("redirects to the path from query parameter", () => {
    window.location.search = "?from=%2Fsettings%2Fgeneral";

    redirectFromFleetDown();

    expect(window.location.href).toBe("/settings/general");
  });

  it("redirects to home page when no from parameter exists", () => {
    window.location.search = "";

    redirectFromFleetDown();

    expect(window.location.href).toBe("/");
  });

  it("redirects to home page when from parameter is empty", () => {
    window.location.search = "?from=";

    redirectFromFleetDown();

    expect(window.location.href).toBe("/");
  });

  it("handles complex paths with query parameters", () => {
    window.location.search = "?from=%2Fminers%3Ffilter%3Dactive";

    redirectFromFleetDown();

    expect(window.location.href).toBe("/miners?filter=active");
  });

  it("preserves hash fragments in redirect URL", () => {
    window.location.search = "?from=%2Fsettings%23security";

    redirectFromFleetDown();

    expect(window.location.href).toBe("/settings#security");
  });

  it("handles paths with query parameters and hash fragments", () => {
    window.location.search = "?from=%2Fminers%3Ffilter%3Dactive%23details";

    redirectFromFleetDown();

    expect(window.location.href).toBe("/miners?filter=active#details");
  });

  // Security tests
  describe("security: prevents open redirect vulnerabilities", () => {
    it("prevents redirect to external URLs", () => {
      window.location.search = "?from=https://evil.com";

      redirectFromFleetDown();

      expect(window.location.href).toBe("/");
    });

    it("prevents redirect to protocol-relative URLs", () => {
      window.location.search = "?from=//evil.com";

      redirectFromFleetDown();

      expect(window.location.href).toBe("/");
    });

    it("prevents redirect to JavaScript URLs", () => {
      window.location.search = "?from=javascript:alert(1)";

      redirectFromFleetDown();

      expect(window.location.href).toBe("/");
    });

    it("allows valid relative paths", () => {
      window.location.search = "?from=%2Fsettings";

      redirectFromFleetDown();

      expect(window.location.href).toBe("/settings");
    });
  });
});
