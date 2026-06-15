import { expect, Page } from "@playwright/test";

const FAKE_PROTO_RIG_SERIAL_PREFIX = "PROTO-SIM-";

type TestingSystemInfoResponse = {
  "system-info": {
    cb_sn?: string;
    manufacturer?: string;
    product_name?: string;
    model?: string;
  };
};

type TestingAuthState = {
  password: string;
  defaultPassword: string;
  onboarded: boolean;
};

export class AuthStateHelper {
  private hasValidatedSimulatorTarget = false;

  constructor(private page: Page) {}

  private async ensureAppLoaded() {
    if (this.page.url() !== "about:blank") {
      return;
    }

    await this.page.goto("/");
  }

  private async assertSafeSimulatorTarget() {
    await this.ensureAppLoaded();

    if (this.hasValidatedSimulatorTarget) {
      return;
    }

    const data = (await this.page.evaluate(async () => {
      const response = await fetch("/api/v1/system");

      if (!response.ok) {
        throw new Error(`Failed to load simulator system info: ${response.status} ${response.statusText}`);
      }

      return (await response.json()) as TestingSystemInfoResponse;
    })) as TestingSystemInfoResponse;
    const systemInfo = data["system-info"];
    const serialNumber = systemInfo.cb_sn ?? "";

    if (!serialNumber.startsWith(FAKE_PROTO_RIG_SERIAL_PREFIX)) {
      throw new Error(
        `Refusing to mutate auth state on non-simulator target "${serialNumber || "unknown"}" (${systemInfo.manufacturer ?? "unknown"} ${systemInfo.product_name ?? systemInfo.model ?? "unknown"}).`,
      );
    }

    this.hasValidatedSimulatorTarget = true;
  }

  async setState({ password, defaultPassword, onboarded }: TestingAuthState) {
    await this.assertSafeSimulatorTarget();

    const response = await this.page.evaluate(
      async (nextState) => {
        const request = await fetch("/api/v1/testing/auth-state", {
          method: "PUT",
          headers: {
            "Content-Type": "application/json",
          },
          body: JSON.stringify({
            password: nextState.password,
            default_password: nextState.defaultPassword,
            onboarded: nextState.onboarded,
          }),
        });

        return {
          ok: request.ok,
          status: request.status,
          statusText: request.statusText,
          body: await request.text(),
        };
      },
      { password, defaultPassword, onboarded },
    );

    expect(
      response.ok,
      `Failed to seed fake-rig auth state: ${response.status} ${response.statusText} ${response.body}`,
    ).toBeTruthy();
  }
}
