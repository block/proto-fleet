import { ReactNode } from "react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";

import App from "./App";
import { FleetOnboardingStatus } from "@/protoFleet/api/generated/onboarding/v1/onboarding_pb";
// TODO: Update this test to work with Zustand store instead of React Context

// Mock the API call for onboarding status
let mockedOnboardingStatus: FleetOnboardingStatus | null = null;
vi.mock("@/protoFleet/api/useOnboardedStatus", () => ({
  useOnboardedStatus: vi.fn(() => ({
    poolConfigured: mockedOnboardingStatus?.poolConfigured ?? false,
    devicePaired: mockedOnboardingStatus?.devicePaired ?? false,
    statusLoaded: true,
    refetch: vi.fn(() => Promise.resolve(mockedOnboardingStatus)),
  })),
}));

// Mock AppLayout component for UI testing
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

// Global test state for auth token validity
// TODO: Re-enable when tests are updated to work with Zustand
// let isValidToken = true;

// TODO: Update this test to work with Zustand store instead of React Context
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

  // Setup function to render the app with a router
  const renderWithRouter = (initialPath = "/") => {
    const router = createMemoryRouter(createRoutes(), {
      initialEntries: [initialPath],
    });

    // TODO: Create the auth context with the test token state
    // This needs to be updated to work with Zustand store
    return render(
      // <AuthContext.Provider
      //   value={{
      //     authTokens: {
      //       accessToken: {
      //         value: isValidToken ? "valid-token" : "",
      //         expiry: isValidToken
      //           ? new Date(Date.now() + 3600000) // Valid for 1 hour
      //           : new Date(Date.now() - 3600000), // Expired 1 hour ago
      //       },
      //     },
      //     setAuthTokens: vi.fn(),
      //     username: "admin",
      //     setUsername: vi.fn(),
      //     loading: false,
      //     logout: vi.fn(),
      //   }}
      // >
      <RouterProvider router={router} />,
      // </AuthContext.Provider>,
    );
  };

  beforeEach(() => {
    vi.clearAllMocks();
    // isValidToken = true;
    mockedOnboardingStatus = null;
  });

  describe("Authentication routing", () => {
    test("should allow access to protected routes with valid token", async () => {
      // isValidToken = true;
      renderWithRouter("/");

      // Should show home page with valid token
      await waitFor(() => {
        expect(screen.getByTestId("home-page")).toBeInTheDocument();
      });

      // Home page should use AppLayout
      expect(screen.getByTestId("app-layout")).toBeInTheDocument();
    });

    test("should redirect to auth page with invalid token", async () => {
      // isValidToken = false;
      renderWithRouter("/");

      // Should redirect to auth page with invalid token
      await waitFor(() => {
        expect(screen.getByTestId("auth-page")).toBeInTheDocument();
      });
    });

    test("should always allow access to auth page regardless of token", async () => {
      // isValidToken = false;
      renderWithRouter("/auth");

      // Should not redirect when already on auth page
      await waitFor(() => {
        expect(screen.getByTestId("auth-page")).toBeInTheDocument();
      });
    });
  });

  // describe("Onboarding routing", () => {
  //   test("should redirect to miners onboarding when devicePaired is false", async () => {
  //     isValidToken = true;
  //     mockedOnboardingStatus = {
  //       devicePaired: false,
  //       poolConfigured: false,
  //     } as FleetOnboardingStatus;

  //     renderWithRouter("/");

  //     // Should redirect to onboarding/miners
  //     await waitFor(() => {
  //       expect(
  //         screen.getByTestId("onboarding-miners-page"),
  //       ).toBeInTheDocument();
  //     });
  //   });

  //   test("should redirect to mining-pool onboarding when poolConfigured is false", async () => {
  //     isValidToken = true;
  //     mockedOnboardingStatus = {
  //       devicePaired: true,
  //       poolConfigured: false,
  //     } as FleetOnboardingStatus;

  //     renderWithRouter("/");

  //     // Should redirect to onboarding/mining-pool
  //     await waitFor(() => {
  //       expect(screen.getByTestId("mining-pool-page")).toBeInTheDocument();
  //     });
  //   });

  //   test("should not redirect when onboarding is complete", async () => {
  //     isValidToken = true;
  //     mockedOnboardingStatus = {
  //       devicePaired: true,
  //       poolConfigured: true,
  //     } as FleetOnboardingStatus;

  //     renderWithRouter("/");

  //     // Should remain on home page
  //     await waitFor(() => {
  //       expect(screen.getByTestId("home-page")).toBeInTheDocument();
  //     });
  //   });

  //   test("should not redirect when onboarding status is still loading", async () => {
  //     isValidToken = true;
  //     mockedOnboardingStatus = null; // Loading state

  //     renderWithRouter("/");

  //     // Should remain on home page
  //     await waitFor(() => {
  //       expect(screen.getByTestId("home-page")).toBeInTheDocument();
  //     });
  //   });
  // });

  // describe("Combined auth and onboarding behavior", () => {
  //   test("should prioritize auth redirect over onboarding redirect", async () => {
  //     isValidToken = false;
  //     mockedOnboardingStatus = {
  //       devicePaired: false,
  //       poolConfigured: true,
  //     } as FleetOnboardingStatus;

  //     renderWithRouter("/");

  //     // Should redirect to auth page, not to onboarding page
  //     await waitFor(() => {
  //       expect(screen.getByTestId("auth-page")).toBeInTheDocument();
  //     });
  //   });

  //   test("should process onboarding after successful auth", async () => {
  //     // First render with invalid token
  //     isValidToken = false;
  //     renderWithRouter("/");

  //     // Should redirect to auth
  //     await waitFor(() => {
  //       expect(screen.getByTestId("auth-page")).toBeInTheDocument();
  //     });

  //     // Now simulate login success and onboarding check
  //     isValidToken = true;
  //     mockedOnboardingStatus = {
  //       devicePaired: false,
  //       poolConfigured: false,
  //     } as FleetOnboardingStatus;

  //     // Re-render with the updated state
  //     renderWithRouter("/");

  //     // Should now redirect to onboarding
  //     await waitFor(() => {
  //       expect(
  //         screen.getByTestId("onboarding-miners-page"),
  //       ).toBeInTheDocument();
  //     });
  //   });
  // });
});
