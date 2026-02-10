// NOTE: eslint incorrectly identifies 'use' as react hook
/* eslint-disable react-hooks/rules-of-hooks */
import { test as base } from "@playwright/test";
import { CommonSteps } from "../helpers/commonSteps";
import { AuthPage } from "../pages/auth";
import { HomePage } from "../pages/home";
import { poolsPage } from "../pages/pools";

type PageFixtures = {
  authPage: AuthPage;
  homePage: HomePage;
  poolPage: poolsPage;
  commonSteps: CommonSteps;
};

export const test = base.extend<PageFixtures>({
  authPage: async ({ page, isMobile }, use) => {
    await use(new AuthPage(page, isMobile));
  },
  homePage: async ({ page, isMobile }, use) => {
    await use(new HomePage(page, isMobile));
  },
  poolPage: async ({ page, isMobile }, use) => {
    await use(new poolsPage(page, isMobile));
  },
  commonSteps: async ({ authPage }, use) => {
    await use(new CommonSteps(authPage));
  },
});

export const expect = test.expect;
