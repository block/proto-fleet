import { expect, type Locator } from "@playwright/test";
import { BasePage } from "./base";

const AUTH_DEBUG_ENABLED = process.env.E2E_AUTH_DEBUG === "true";
const SESSION_VALIDATION_PATH = "/api-proxy/onboarding.v1.OnboardingService/GetFleetOnboardingStatus";

export class AuthPage extends BasePage {
  private invalidCredentialsContainer() {
    return this.page.getByTestId("error");
  }

  private async readAuthStateSnapshot(loggedInMarker: Locator, loggedInMarkerName: string) {
    const usernameInput = this.page.locator("#username");
    const passwordInput = this.page.locator("#password");
    const loginFormVisible = await usernameInput.isVisible().catch(() => false);
    const loggedInMarkerVisible = await loggedInMarker.isVisible().catch(() => false);
    const activeElement = await this.page.evaluate(() => {
      const element = document.activeElement as HTMLElement | null;
      if (!element) {
        return null;
      }

      return {
        id: element.id || null,
        tagName: element.tagName,
        testId: element.getAttribute("data-testid"),
      };
    });

    return {
      activeElement,
      loggedInMarkerName,
      loggedInMarkerVisible,
      loginFormVisible,
      passwordLength: ((await passwordInput.inputValue().catch(() => "")) || "").length,
      url: this.page.url(),
      usernameLength: ((await usernameInput.inputValue().catch(() => "")) || "").length,
    };
  }

  private async logAuthState(label: string, loggedInMarker: Locator, loggedInMarkerName: string, timeoutMs: number) {
    if (!AUTH_DEBUG_ENABLED) {
      return;
    }

    const snapshot = await this.readAuthStateSnapshot(loggedInMarker, loggedInMarkerName);
    console.warn(
      `[auth-debug] ${label} ${JSON.stringify({
        ...snapshot,
        isMobile: this.isMobile,
        timeoutMs,
      })}`,
    );
  }

  private async hasValidSession(): Promise<boolean> {
    try {
      const response = await this.page.context().request.post(SESSION_VALIDATION_PATH, {
        data: {},
        headers: {
          "Connect-Protocol-Version": "1",
          "Content-Type": "application/json",
        },
      });
      return response.ok();
    } catch {
      return false;
    }
  }

  async isAlreadyLoggedIn(timeoutMs = 5000): Promise<boolean> {
    const loggedInMarkerName = this.isMobile ? "navigation-menu-button" : "logout-button";
    const loggedInMarker = this.page.getByTestId(loggedInMarkerName);
    const loginForm = this.page.locator(`//input[@id='username']`);

    await this.logAuthState("isAlreadyLoggedIn:before-wait", loggedInMarker, loggedInMarkerName, timeoutMs);

    try {
      await expect(loggedInMarker.or(loginForm)).toBeVisible({ timeout: timeoutMs });
    } catch (err) {
      await this.logAuthState("isAlreadyLoggedIn:wait-failed", loggedInMarker, loggedInMarkerName, timeoutMs);
      // Only swallow timeouts so selector regressions propagate instead of
      // silently falling through to the login flow.
      if (err instanceof Error && /Timeout/i.test(err.message)) {
        return false;
      }
      throw err;
    }

    await this.logAuthState("isAlreadyLoggedIn:after-wait", loggedInMarker, loggedInMarkerName, timeoutMs);
    if (!(await loggedInMarker.isVisible().catch(() => false))) {
      return false;
    }

    return await this.hasValidSession();
  }

  async inputUsername(username: string) {
    await this.page.locator(`//input[@id='username']`).fill(username);
  }

  async inputPassword(password: string) {
    const passwordInput = this.page.locator(`//input[@id='password']`);
    await passwordInput.clear();
    await passwordInput.fill(password);
  }

  async clickLogin() {
    await this.page.locator(`//button[@data-testid="login-button"]`).click();
  }

  async validateRedirectedToAuth() {
    await expect(this.page).toHaveURL(/.*\/auth/);
  }

  async gotoAuthPage() {
    const loginForm = this.page.locator(`//input[@id='username']`);
    await this.page.goto("/auth");
    await expect(this.page).toHaveURL(/.*\/auth/);
    await expect(loginForm).toBeVisible();
  }

  async inputNewPassword(password: string) {
    await this.page.locator(`//input[@id='newPassword']`).fill(password);
  }

  async inputConfirmPassword(password: string) {
    await this.page.locator(`//input[@id='confirmPassword']`).fill(password);
  }

  async clickContinue() {
    await this.clickButton("Continue");
  }

  async clickLoginButton() {
    await this.clickButton("Login");
  }

  async completeInitialSetupOrLogin(username: string, password: string) {
    await this.inputUsername(username);
    await this.inputPassword(password);

    const continueButton = this.page.getByRole("button", { name: "Continue", exact: true });
    const loginButton = this.page.getByTestId("login-button");

    await expect(continueButton.or(loginButton)).toBeVisible();

    if (await continueButton.isVisible().catch(() => false)) {
      await continueButton.click();
    } else {
      await this.clickLogin();
    }

    await this.validateLoggedIn();
  }

  async clickPasswordVisibilityToggle() {
    await this.page.locator(`//*[@data-testid="eye-icon"]`).click();
  }

  async validateInvalidCredentials() {
    await expect(this.invalidCredentialsContainer()).not.toHaveClass(/hidden/);
    await expect(
      this.invalidCredentialsContainer().getByText("Invalid credentials entered.", { exact: true }),
    ).toBeVisible();
  }

  async validateInvalidCredentialsNotVisible() {
    await expect(this.invalidCredentialsContainer()).toHaveClass(/hidden/);
  }

  async validateUpdatePasswordTitle() {
    await this.validateTitle("Update Your Password");
  }

  async validatePasswordSaved() {
    await this.validateTitle("Password saved");
  }

  async clickCreateAccount() {
    await this.clickButton("Create an account");
  }

  getCreateCredentialsForm() {
    const heading = this.page.getByText("Create your username and password", { exact: true });

    return this.page
      .locator("div")
      .filter({ has: heading })
      .filter({ has: this.page.locator("#username") })
      .filter({ has: this.page.getByRole("button", { name: "Continue", exact: true }) })
      .first();
  }

  async validateCreateCredentialsPrompt() {
    await expect(this.page.getByText("Create your username and password")).toBeVisible();
  }

  async clickGetStarted() {
    await this.clickButton("Get started");
  }
}
