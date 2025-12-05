// import fs from "fs";
// import path from "path";
import { test } from "../fixtures/pageFixtures";
// import { testConfig } from "../config/test.config";

test.describe("Playwright base tests", () => {
  test.describe.configure({ mode: "default" });

  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  ////// POC for API request/response capture and mocking
  // test('capture gRPC request + response', async ({ page }, testInfo) => {
  //   // Capture request
  //   const requestPromise = page.waitForRequest(/ListMinerStateSnapshots/);

  //   // Capture response
  //   const responsePromise = page.waitForResponse(/ListMinerStateSnapshots/);

  //   await page.locator(`//input[@id='username']`).fill('admin');
  //   await page.locator(`//input[@id='password']`).fill('Pass123!');
  //   await page.locator(`//button[@data-testid="login-button"]`).click();
  //   await page.locator(`//*[@data-testid="navigation-menu"]//*[@href='/miners']`).click();
  //   await expect(page).toHaveURL(/.*\/miners/);
  //   await page.locator(`//h2[text()='Miners']`).waitFor({ state: 'visible' });
  //   const rows = page.locator(`//*[@data-testid='list-body']/tr`);
  //   await expect(rows).toHaveCount(12);

  //   const request = await requestPromise;
  //   const response = await responsePromise;

  //   // Extract JSON payloads
  //   const requestJson = request.postDataJSON();
  //   const responseJson = await response.json();

  //   // Attach to the test report
  //   await testInfo.attach('grpc-request.json', {
  //     body: JSON.stringify(requestJson, null, 2),
  //     contentType: 'application/json',
  //   });

  //   await testInfo.attach('grpc-response.json', {
  //     body: JSON.stringify(responseJson, null, 2),
  //     contentType: 'application/json',
  //   });

  //   // Optional example assertion
  //   expect(responseJson).toBeTruthy();
  // });

  // test('AUTO-RECORD jsons', async ({ page }, testInfo) => {
  //   page.on('response', async (res) => {
  //     if (!res.url().includes("ListMinerStateSnapshots")) return;

  //     try {
  //       const json = await res.json();
  //       const req = res.request().postDataJSON();
  //       const mode = req?.dataMode ?? "unknown";

  //       const filePath = path.join("tests/fixtures", `${mode}.json`);
  //       fs.writeFileSync(filePath, JSON.stringify(json, null, 2));
  //       console.log(`Saved mock: ${filePath}`);

  //     } catch (err) {
  //       console.warn("Could not capture response for:", res.url());
  //     }
  //   });

  //   await page.locator(`//input[@id='username']`).fill('admin');
  //   await page.locator(`//input[@id='password']`).fill('Pass123!');
  //   await page.locator(`//button[@data-testid="login-button"]`).click();
  //   await page.locator(`//*[@data-testid="navigation-menu"]//*[@href='/miners']`).click();
  //   await expect(page).toHaveURL(/.*\/miners/);
  //   await page.locator(`//h2[text()='Miners']`).waitFor({ state: 'visible' });
  //   await page.waitForTimeout(5000);
  //   const rows = page.locator(`//*[@data-testid='list-body']/tr`);
  //   await expect(rows).toHaveCount(12);
  // });

  // test("mock all miner gRPC responses", async ({ page, authPage }) => {
  //   await page.route(/ListMinerStateSnapshots/, async (route) => {
  //     const req = route.request();
  //     const body = req.postDataJSON();
  //     const mode = body.dataMode;

  //     const filePath = path.join("tests", "fixtures", `${mode}.json`);

  //     // If no file exists → DO NOT MOCK → let backend handle it
  //     if (!mode || !fs.existsSync(filePath)) {
  //       return route.continue();
  //     }

  //     const mockJson = fs.readFileSync(filePath, "utf8");

  //     return route.fulfill({
  //       status: 200,
  //       contentType: "application/json",
  //       body: mockJson,
  //     });
  //   });

  //   // Login and navigate to miners page
  //   await authPage.inputUsername(testConfig.users.admin.username);
  //   await authPage.inputPassword(testConfig.users.admin.password);
  //   await authPage.clickLogin();
  //   await authPage.navigateToMinersPage();
  //   await page.locator(`//h2[text()='Miners']`).waitFor({ state: "visible" });

  //   // Verify that the mocked data is displayed
  //   await page.locator(`//*[contains(text(),'UGA BUGA')]`).scrollIntoViewIfNeeded();
  // });
});

// COMPLETED:
// ✅ Code is in /client/e2eTests
// ✅ Separate spec files per feature (auth, teamAccounts, miners, miningPools)
// ✅ Base page object structure created with fixture pattern
// ✅ CI pipeline configured (.github/workflows/protofleet-e2e-tests.yml)
//
// TODO (Future enhancements):
// - Add proper README with setup and usage instructions
// - Consider implementing API request/response mocking (POC examples above)
// - Add visual regression testing for critical UI components
