/* eslint-disable playwright/no-conditional-in-test -- scripted page double routes locator calls by selector */
import { type Page } from "@playwright/test";
import { expect, test } from "../fixtures/pageFixtures";
import { BasePage } from "../pages/base";

test.describe("Proto Fleet - Base page", () => {
  test("mobile logout tolerates an auth redirect while opening navigation", async () => {
    let loginFormVisible = false;
    let navigationMenuClicks = 0;
    let navigationMenuClickOptions: { timeout?: number } | undefined;
    let authPageNavigations = 0;

    const page = {
      url: () => "http://localhost:5173/fleet/miners",
      locator: (selector: string) => {
        if (selector !== "#username") {
          throw new Error(`Unexpected locator: ${selector}`);
        }

        return {
          isVisible: async () => loginFormVisible,
        };
      },
      getByTestId: (testId: string) => {
        switch (testId) {
          case "logout-button":
            return {
              isVisible: async () => false,
              click: async () => {
                throw new Error("Logout should not be clicked after the auth redirect");
              },
            };
          case "navigation-menu":
            return {
              isVisible: async () => false,
            };
          case "navigation-menu-button":
            return {
              isVisible: async () => true,
              click: async (options?: { timeout?: number }) => {
                navigationMenuClicks += 1;
                navigationMenuClickOptions = options;
                loginFormVisible = true;
                throw new Error("Navigation button disappeared during the auth redirect");
              },
            };
          default:
            throw new Error(`Unexpected test id: ${testId}`);
        }
      },
      goto: async () => {
        authPageNavigations += 1;
      },
    } as unknown as Page;

    await new BasePage(page, true).logout();

    expect(navigationMenuClicks).toBe(1);
    expect(navigationMenuClickOptions).toEqual({ timeout: 2_000 });
    expect(authPageNavigations).toBe(0);
  });

  test("mobile logout waits for the logged-out state after clicking logout", async () => {
    let loginFormChecks = 0;
    let logoutClicks = 0;

    const page = {
      url: () => "http://localhost:5173/fleet/miners",
      locator: (selector: string) => {
        if (selector !== "#username") {
          throw new Error(`Unexpected locator: ${selector}`);
        }

        return {
          isVisible: async () => {
            loginFormChecks += 1;
            return loginFormChecks >= 3;
          },
        };
      },
      getByTestId: (testId: string) => {
        if (testId !== "logout-button") {
          throw new Error(`Unexpected test id: ${testId}`);
        }

        return {
          isVisible: async () => true,
          click: async () => {
            logoutClicks += 1;
          },
        };
      },
    } as unknown as Page;

    await new BasePage(page, true).logout();

    expect(loginFormChecks).toBe(3);
    expect(logoutClicks).toBe(1);
  });
});
