/* eslint-disable playwright/expect-expect */
import path from "path";
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";

export const adminStorageStatePath = path.join(__dirname, "..", "playwright", ".auth", "admin.json");

test("save admin auth storage state @setup", async ({ page, authPage }) => {
  await page.goto("/");

  await authPage.inputUsername(testConfig.users.admin.username);
  await authPage.inputPassword(testConfig.users.admin.password);
  await authPage.clickLogin();
  await authPage.validateLoggedIn();

  await page.context().storageState({ path: adminStorageStatePath });
});
