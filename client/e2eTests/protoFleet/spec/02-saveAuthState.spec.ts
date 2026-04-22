/* eslint-disable playwright/expect-expect */
import fs from "fs/promises";
import path from "path";
import { fileURLToPath } from "url";
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";

const specDir = path.dirname(fileURLToPath(import.meta.url));
const adminStorageStatePath = path.join(specDir, "..", "playwright", ".auth", "admin.json");

test("save admin auth storage state @setup", async ({ page, authPage }) => {
  await page.goto("/");

  await authPage.inputUsername(testConfig.users.admin.username);
  await authPage.inputPassword(testConfig.users.admin.password);
  await authPage.clickLogin();
  await authPage.validateLoggedIn();

  // playwright/.auth/ is gitignored and absent on fresh checkouts.
  await fs.mkdir(path.dirname(adminStorageStatePath), { recursive: true });
  await page.context().storageState({ path: adminStorageStatePath });
});
