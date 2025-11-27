import fs from "fs";
import path from "path";
import { expect, test } from "@playwright/test";
test.describe("Playwright base tests", () => {
  test.describe.configure({ mode: "default" });

  test.beforeEach(async ({ page }) => {
    await page.goto("http://localhost:5173");
    await expect(page).toHaveURL(/localhost:5173/);
  });

  test("Sign in", async ({ page }) => {
    await page.locator(`//input[@id='username']`).fill("admin");
    await page.locator(`//input[@id='password']`).fill("Parole123!");
    await page.locator(`//button[@data-testid="login-button"]`).click();
    await expect(page.locator(`//*[@data-testid="navigation-menu"]//*[@href='/miners']`)).toBeVisible();
  });

  //// Will figure out later proper strategy for running tests that have high risk of impacting the other ones
  // test('Put miners to sleep', async ({ page }) => {
  //     await page.locator(`//input[@id='username']`).fill('admin');
  //     await page.locator(`//input[@id='password']`).fill('Parole123!');
  //     await page.locator(`//button[@data-testid="login-button"]`).click();
  //     await page.locator(`//*[@data-testid="navigation-menu"]//*[@href='/miners']`).click();
  //     await expect(page).toHaveURL(/.*\/miners/);
  //     await page.locator(`//h2[text()='Miners']`).waitFor({ state: 'visible' });
  //     const rows = page.locator(`//*[@data-testid='list-body']/tr`);
  //     await expect(rows).toHaveCount(12);

  //     await page.locator(`//*[@data-testid="list-header"]//input[@type="checkbox"]`).click();
  //     await page.locator(`//*[@data-testid="actions-menu-button"]`).click();
  //     await page.locator(`//*[@data-testid="shutdown-popover-button"]`).click();
  //     await page.locator(`//*[@data-testid="shutdown-confirm-button"]`).click();

  //     const rowCount = await rows.count();
  //     for (let i = 1; i <= rowCount; i++) {
  //         await rows.nth(i).scrollIntoViewIfNeeded();
  //         // await expect(rows.nth(i)).toContainText('Sleeping', { timeout: 15000 });
  //         await expect(rows.nth(i)).toContainText('Sleeping');
  //     }
  // });

  test("Configure mining pool", async ({ page }) => {
    // login
    await page.locator(`//input[@id='username']`).fill("admin");
    await page.locator(`//input[@id='password']`).fill("Parole123!");
    await page.locator(`//button[@data-testid="login-button"]`).click();

    // navigate to mining pools settings
    await page.locator(`//*[@data-testid="navigation-menu"]//*[@href='/settings']`).click();
    await expect(page).toHaveURL(/.*\/settings/);
    await page.locator(`//*[@data-testid="secondary-nav"]//*[@href="/settings/mining-pools"]`).click();
    await expect(page).toHaveURL(/.*\/mining-pools/);
    await expect(page.getByText(`Update your mining pools`)).toBeVisible();

    // configure mining pool
    await page.locator(`//*[@data-testid="pool-0-add-button"]`).click();
    await expect(page.locator(`//*[@data-testid="modal"]`).getByText(`Default mining pool`).first()).toBeVisible();
    await page.locator(`//input[@id='url 0']`).fill("stratum+tcp://eu1.examplepool.com:3333");
    await page.locator(`//input[@id='username 0']`).fill("myworker");

    // test connection
    await page.locator(`//button//*[text()='Test connection']`).click();
    await expect(page.getByText(`We couldn’t connect with your default pool.`)).toBeVisible();
    await page.locator(`//*[@data-testid="modal"]`).locator(`//button//*[text()='Dismiss']`).click();

    // save & validate pool url
    await page.locator(`//*[@data-testid="pool-save-button"]`).click();
    await expect(page.locator(`//*[@data-testid="pool-0-saved-url"]`)).toHaveText(
      "stratum+tcp://eu1.examplepool.com:3333",
    );

    // configure mining pool
    await page.locator(`//*[@data-testid="pool-0-add-button"]`).click();
    await expect(page.locator(`//*[@data-testid="modal"]`).getByText(`Default mining pool`).first()).toBeVisible();

    // empty url and username
    await page.locator(`//input[@id='url 0']`).fill("");
    await page.locator(`//input[@id='username 0']`).fill("");

    // save pool
    await page.locator(`//*[@data-testid="pool-save-button"]`).click();
    await expect(
      page.locator(`//*[text()='Not configured'][preceding-sibling::*[text()='Default pool']]`),
    ).toBeVisible();
  });

  test("Wake miners up", async ({ page }) => {
    await page.locator(`//input[@id='username']`).fill("admin");
    await page.locator(`//input[@id='password']`).fill("Parole123!");
    await page.locator(`//button[@data-testid="login-button"]`).click();
    await page.locator(`//*[@data-testid="navigation-menu"]//*[@href='/miners']`).click();
    await expect(page).toHaveURL(/.*\/miners/);
    await page.locator(`//h2[text()='Miners']`).waitFor({ state: "visible" });
    const rows = page.locator(`//*[@data-testid='list-body']/tr`);
    await expect(rows).toHaveCount(12);

    await page.locator(`//*[@data-testid="list-header"]//input[@type="checkbox"]`).click();
    await page.locator(`//*[@data-testid="actions-menu-button"]`).click();
    await page.locator(`//*[@data-testid="wake-up-popover-button"]`).click();
    await page.locator(`//*[@data-testid="wake-up-confirm-button"]`).click();
    await expect(page.locator(`text=Update in progress`)).toBeVisible({
      timeout: 2000,
    });
    await expect(page.locator(`text=Update in progress`)).not.toBeVisible({
      timeout: 15000,
    });

    const rowCount = await rows.count();
    for (let i = 1; i <= rowCount; i++) {
      await rows.nth(i).scrollIntoViewIfNeeded();
      await expect(rows.nth(i).locator("//td[5]")).toContainText("Hashing");
    }
  });

  ////// POC for API request/response capture and mocking
  // test('capture gRPC request + response', async ({ page }, testInfo) => {
  //   // Capture request
  //   const requestPromise = page.waitForRequest(/ListMinerStateSnapshots/);

  //   // Capture response
  //   const responsePromise = page.waitForResponse(/ListMinerStateSnapshots/);

  //   await page.locator(`//input[@id='username']`).fill('admin');
  //   await page.locator(`//input[@id='password']`).fill('Parole123!');
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
  //   await page.locator(`//input[@id='password']`).fill('Parole123!');
  //   await page.locator(`//button[@data-testid="login-button"]`).click();
  //   await page.locator(`//*[@data-testid="navigation-menu"]//*[@href='/miners']`).click();
  //   await expect(page).toHaveURL(/.*\/miners/);
  //   await page.locator(`//h2[text()='Miners']`).waitFor({ state: 'visible' });
  //   await page.waitForTimeout(5000);
  //   const rows = page.locator(`//*[@data-testid='list-body']/tr`);
  //   await expect(rows).toHaveCount(12);
  // });

  test("mock all miner gRPC responses", async ({ page }) => {
    await page.route(/ListMinerStateSnapshots/, async (route) => {
      const req = route.request();
      const body = req.postDataJSON();
      const mode = body.dataMode;

      const filePath = path.join("tests", "fixtures", `${mode}.json`);

      // If no file exists → DO NOT MOCK → let backend handle it
      if (!mode || !fs.existsSync(filePath)) {
        return route.continue();
      }

      const mockJson = fs.readFileSync(filePath, "utf8");

      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: mockJson,
      });
    });

    // Login and navigate to miners page
    await page.locator(`//input[@id='username']`).fill("admin");
    await page.locator(`//input[@id='password']`).fill("Parole123!");
    await page.locator(`//button[@data-testid="login-button"]`).click();
    await page.locator(`//*[@data-testid="navigation-menu"]//*[@href='/miners']`).click();
    await expect(page).toHaveURL(/.*\/miners/);
    await page.locator(`//h2[text()='Miners']`).waitFor({ state: "visible" });

    // Verify that the mocked data is displayed
    await page.locator(`//*[contains(text(),'UGA BUGA')]`).scrollIntoViewIfNeeded();
  });

  test("Onboarding", async ({ page }) => {
    // await page.locator(`//button[descendant::*[text()='Create an account']]`).click();
    await page.getByRole("button", { name: "Create an account" }).click();
    await expect(page.getByText("Create your username and password")).toBeVisible();
    await page.locator(`//input[@id='username']`).fill("admin");
    await page.locator(`//input[@id='password']`).fill("Parole123!");
    await page.getByRole("button", { name: "Continue" }).click();
    await page.getByRole("button", { name: "Get started" }).click();
    await page.getByRole("button", { name: "Find miners" }).first().click();
    await page.getByRole("button", { name: "Continue with 12 miners" }).click();
    await page.locator(`//*[@data-testid="navigation-menu"]//*[@href='/miners']`).click();
    await expect(page).toHaveURL(/.*\/miners/);
    await page.locator(`//h2[text()='Miners']`).waitFor({ state: "visible" });
    const rows = page.locator(`//*[@data-testid='list-body']/tr`);
    await expect(rows).toHaveCount(12);
  });

  test("Team accounts - add team member", async ({ page }) => {
    await page.locator(`//input[@id='username']`).fill("admin");
    await page.locator(`//input[@id='password']`).fill("Parole123!");
    await page.locator(`//button[@data-testid="login-button"]`).click();
    await page.locator(`//*[@data-testid="navigation-menu"]//*[@href='/settings']`).click();
    await expect(page).toHaveURL(/.*\/settings/);
    await page.locator(`//*[@data-testid="secondary-nav"]//*[@href="/settings/team"]`).click();
    await expect(page).toHaveURL(/.*\/team/);
    await expect(page.locator(`//*[contains(@class,'heading')][text()='Team']`)).toBeVisible();
    await page.getByRole("button", { name: "Add team member" }).click();
    const randomCode = Math.random().toString(36).substring(2, 9);
    const username = `member${randomCode}`;
    await page.locator(`//input[@id='username']`).fill(username);
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.locator(`//*[@data-testid="modal"]`).getByText("Member added")).toBeVisible();
    await expect(page.locator(`//button[@aria-label="Copy password"]`)).toBeVisible();
    await page.getByRole("button", { name: "Done" }).click();
    const memberRow = page.locator(`//*[@data-testid='list-body']/tr`).filter({
      has: page.locator(`//td[@data-testid='username']//*[text()='${username}']`),
    });
    await expect(memberRow.locator(`//td[@data-testid='role']`)).toHaveText("Admin");
    await expect(memberRow.locator(`//td[@data-testid='lastLoginAt']`)).toHaveText("Never");
  });

  test("Team accounts - new member log in", async ({ page }) => {
    await page.locator(`//input[@id='username']`).fill("admin");
    await page.locator(`//input[@id='password']`).fill("Parole123!");
    await page.locator(`//button[@data-testid="login-button"]`).click();
    await page.locator(`//*[@data-testid="navigation-menu"]//*[@href='/settings']`).click();
    await expect(page).toHaveURL(/.*\/settings/);
    await page.locator(`//*[@data-testid="secondary-nav"]//*[@href="/settings/team"]`).click();
    await expect(page).toHaveURL(/.*\/team/);
    await expect(page.locator(`//*[contains(@class,'heading')][text()='Team']`)).toBeVisible();

    await page.getByRole("button", { name: "Add team member" }).click();
    const randomCode = Math.random().toString(36).substring(2, 9);
    const username = `member${randomCode}`;
    await page.locator(`//input[@id='username']`).fill(username);
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.locator(`//*[@data-testid="modal"]`).getByText("Member added")).toBeVisible();
    const tempPassword = await page.locator(`//*[@data-testid="temporary-password"]`).innerText();
    await page.getByRole("button", { name: "Done" }).click();
    await expect(page.locator(`//td[@data-testid='username']//*[text()='${username}']`)).toBeVisible();

    await page.locator(`//*[@data-testid="logout-button"]`).click();
    await expect(page).toHaveURL(/.*\/auth/);
    await page.locator(`//input[@id='username']`).fill(username);
    await page.locator(`//input[@id='password']`).fill(tempPassword);
    await page.locator(`//button[@data-testid="login-button"]`).click();
    await page.locator(`//input[@id='newPassword']`).fill("Password123!");
    await page.locator(`//input[@id='confirmPassword']`).fill("Password123!");
    await page.getByRole("button", { name: "Continue" }).click();
    await page.getByRole("button", { name: "Login" }).click();
    await expect(page.locator(`//*[@data-testid="navigation-menu"]//*[@href='/miners']`)).toBeVisible();

    // verify no admin rights
    await page.locator(`//*[@data-testid="navigation-menu"]//*[@href='/settings']`).click();
    await expect(page).toHaveURL(/.*\/settings/);
    await page.locator(`//*[@data-testid="secondary-nav"]//*[@href="/settings/team"]`).click();
    await expect(page).toHaveURL(/.*\/team/);
    await expect(page.getByRole("button", { name: "Add team member" })).toBeHidden();
  });

  test("Team accounts - new member password reset", async ({ page }) => {
    // login
    await page.locator(`//input[@id='username']`).fill("admin");
    await page.locator(`//input[@id='password']`).fill("Parole123!");
    await page.locator(`//button[@data-testid="login-button"]`).click();

    // navigate to settings - team
    await page.locator(`//*[@data-testid="navigation-menu"]//*[@href='/settings']`).click();
    await expect(page).toHaveURL(/.*\/settings/);
    await page.locator(`//*[@data-testid="secondary-nav"]//*[@href="/settings/team"]`).click();
    await expect(page).toHaveURL(/.*\/team/);
    await expect(page.locator(`//*[contains(@class,'heading')][text()='Team']`)).toBeVisible();

    // add team member
    await page.getByRole("button", { name: "Add team member" }).click();
    const randomCode = Math.random().toString(36).substring(2, 9);
    const username = `member${randomCode}`;
    await page.locator(`//input[@id='username']`).fill(username);
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.locator(`//*[@data-testid="modal"]`).getByText("Member added")).toBeVisible();
    const tempPassword1 = await page.locator(`//*[@data-testid="temporary-password"]`).innerText();
    await page.getByRole("button", { name: "Done" }).click();
    const memberRow = page.locator(`//*[@data-testid='list-body']/tr`).filter({
      has: page.locator(`//td[@data-testid='username']//*[text()='${username}']`),
    });

    // reset member password
    await memberRow.locator(`//*[@data-testid="list-actions-trigger"]`).click();
    await page.getByRole("button", { name: "Reset Password" }).click();
    await page.getByRole("button", { name: "Reset member password" }).click();
    await expect(page.locator(`//*[@data-testid="modal"]`).getByText("Password reset")).toBeVisible();
    const tempPassword2 = await page.locator(`//*[@data-testid="temporary-password"]`).innerText();
    await page.getByRole("button", { name: "Done" }).click();

    // log out
    await page.locator(`//*[@data-testid="logout-button"]`).click();
    await expect(page).toHaveURL(/.*\/auth/);

    // log in with initial (wrong) temp password
    await page.locator(`//input[@id='username']`).fill(username);
    await page.locator(`//*[@data-testid="eye-icon"]`).click();
    await page.locator(`//input[@id='password']`).fill(tempPassword1);
    await page.locator(`//button[@data-testid="login-button"]`).click();
    await expect(page.getByText("Invalid credentials entered.")).toBeVisible();

    // log in with new temp password
    await page.locator(`//input[@id='username']`).fill(username);
    await page.locator(`//input[@id='password']`).fill(tempPassword2);
    await page.locator(`//button[@data-testid="login-button"]`).click();
    await expect(page.locator(`//*[contains(@class,'heading')][text()='Update Your Password']`)).toBeVisible();

    // set new password
    await page.locator(`//input[@id='newPassword']`).fill("Password123!");
    await page.locator(`//input[@id='confirmPassword']`).fill("Password123!");
    await page.getByRole("button", { name: "Continue" }).click();
    await expect(page.locator(`//*[contains(@class,'heading')][text()='Password saved']`)).toBeVisible();

    await page.getByRole("button", { name: "Login" }).click();
    await expect(page.locator(`//*[@data-testid="navigation-menu"]//*[@href='/miners']`)).toBeVisible();

    // log out
    await page.locator(`//*[@data-testid="logout-button"]`).click();
    await expect(page).toHaveURL(/.*\/auth/);
  });
});

// TODO
// put this code in /client/e2eTests
// add proper README
// separate spec files per feature
// create base page object structure
// <<after discussion with Marvin>>
// <<investigate and try adding tests into CI pipeline.. setup might be tricky>>
