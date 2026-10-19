import { lazy } from "react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { render, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";
import App from "./App";
import { onboardingClient } from "@/protoFleet/api/clients";
const originalLocation = Object.getOwnPropertyDescriptor(window, "location")!;
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  Object.defineProperty(window, "location", originalLocation);
});
it.each(["/account-invitation", "/account-invitation/", "/ACCOUNT-INVITATION", "/account-invitati%6Fn"])(
  "keeps an invitation fragment out of the health redirect before its lazy page mounts (%s)",
  async (pathname) => {
    vi.stubGlobal(
      "matchMedia",
      vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })),
    );
    const token = "x".repeat(43);
    const location = {
      pathname,
      search: "",
      hash: `#token=${token}`,
      href: "",
      origin: "http://localhost",
    };
    Object.defineProperty(window, "location", { value: location, writable: true, configurable: true });
    vi.spyOn(onboardingClient, "getFleetInitStatus").mockRejectedValue(
      new ConnectError("Backend unavailable", Code.Unavailable),
    );
    const SuspendedInvitation = lazy(() => new Promise<never>(() => {}));
    render(
      <RouterProvider
        router={createMemoryRouter(
          [
            {
              path: "/account-invitation",
              element: (
                <App fullscreen>
                  <SuspendedInvitation />
                </App>
              ),
            },
          ],
          { initialEntries: [`${pathname}#token=${token}`] },
        )}
      />,
    );
    await waitFor(() => expect(location.href).toBe(`/fleet-down?from=${encodeURIComponent(pathname)}`));
    expect(location.href).not.toContain(token);
    expect(location.href).not.toContain("token");
  },
);
