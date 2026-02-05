// NOTE: eslint incorrectly identifies 'use' as react hook
/* eslint-disable react-hooks/rules-of-hooks */
import { test as base } from "@playwright/test";
import { AuthPage } from "../pages/auth";
import { HomePage } from "../pages/home";
import { PoolModalPage } from "../pages/poolModal";

type PageFixtures = {
  authPage: AuthPage;
  homePage: HomePage;
  poolPage: PoolModalPage;
};

export const test = base.extend<PageFixtures>({
  authPage: async ({ page, isMobile }, use) => {
    await use(new AuthPage(page, isMobile));
  },
  homePage: async ({ page, isMobile }, use) => {
    await use(new HomePage(page, isMobile));
  },
  poolPage: async ({ page, isMobile }, use) => {
    await use(new PoolModalPage(page, isMobile));
  },
});

export const expect = test.expect;
