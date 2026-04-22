import { ReactNode } from "react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";

import App from "./App";
import { FleetOnboardingStatus } from "@/protoFleet/api/generated/onboarding/v1/onboarding_pb";

let mockedOnboardingStatus: FleetOnboardingStatus | null = null;
vi.mock("@/protoFleet/api/useOnboardedStatus", () => ({
  useOnboardedStatus: vi.fn(() => ({
    poolConfigured: mockedOnboardingStatus?.poolConfigured ?? false,
    devicePaired: mockedOnboardingStatus?.devicePaired ?? false,
    statusLoaded: true,
    refetch: vi.fn(() => Promise.resolve(mockedOnboardingStatus)),
  })),
}));

vi.mock("@/protoFleet/components/AppLayout", () => ({
  default: ({ children }: { children: ReactNode }) => (
    <div data-testid="app-layout">
      <div>App Layout Header</div>
      {children}
    </div>
  ),
}));

vi.mock("@/protoFleet/routes", () => ({
  getRouteMetadata: vi.fn((pathname) => ({
    title: pathname === "/auth" ? "Auth" : pathname.includes("onboarding") ? "Onboarding" : "Home",
    requireAuth: pathname !== "/auth" && !pathname.includes("/welcome"),
    useAppLayout: !pathname.includes("/auth") && !pathname.includes("/onboarding"),
  })),
}));

describe.skip("App", () => {
  const createRoutes = () => [
    {
      path: "/",
      element: <App />,
      children: [
        {
          index: true,
          element: <div data-testid="home-page">Home Page Content</div>,
        },
        {
          path: "auth",
          element: <div data-testid="auth-page">Auth Page Content</div>,
        },
        {
          path: "miners",
          element: <div data-testid="miners-page">Miners Page Content</div>,
        },
        {
          path: "welcome",
          element: <div data-testid="landing-page">Landing Page Content</div>,
        },
        {
          path: "onboarding/miners",
          element: <div data-testid="onboarding-miners-page">Miners Onboarding Page</div>,
        },
        {
          path: "onboarding/mining-pool",
          element: <div data-testid="mining-pool-page">Mining Pool Page Content</div>,
        },
      ],
    },
  ];

  const renderWithRouter = (initialPath = "/") => {
    const router = createMemoryRouter(createRoutes(), {
      initialEntries: [initialPath],
    });

    return render(<RouterProvider router={router} />);
  };

  beforeEach(() => {
    vi.clearAllMocks();
    mockedOnboardingStatus = null;
  });

  describe("Authentication routing", () => {
    test("should allow access to protected routes with valid token", async () => {
      renderWithRouter("/");

      await waitFor(() => {
        expect(screen.getByTestId("home-page")).toBeInTheDocument();
      });

      expect(screen.getByTestId("app-layout")).toBeInTheDocument();
    });

    test("should redirect to auth page with invalid token", async () => {
      renderWithRouter("/");

      await waitFor(() => {
        expect(screen.getByTestId("auth-page")).toBeInTheDocument();
      });
    });

    test("should always allow access to auth page regardless of token", async () => {
      renderWithRouter("/auth");

      await waitFor(() => {
        expect(screen.getByTestId("auth-page")).toBeInTheDocument();
      });
    });
  });
});
