import { APIRequestContext, expect, Page } from "@playwright/test";

type FirmwareState = {
  status: string;
  currentVersion: string;
  newVersion: string | null;
  previousVersion: string | null;
};

const FAKE_PROTO_RIG_SERIAL_PREFIX = "PROTO-SIM-";
const FIRMWARE_STATUS_TIMEOUT_MS = 20_000;
const FIRMWARE_STATUS_POLL_INTERVAL_MS = 250;

function sleep(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

export class FirmwareHelper {
  private authAccessToken = "";
  private hasValidatedDummyUploadTarget = false;

  constructor(
    private page: Page,
    private request: APIRequestContext,
  ) {}

  async initializeAuthAccessToken() {
    const authStorage = await this.page.evaluate(() => localStorage.getItem("proto-os-auth"));
    if (!authStorage) {
      throw new Error("proto-os-auth is missing from localStorage");
    }

    const parsedStorage = JSON.parse(authStorage) as {
      state?: {
        auth?: {
          authTokens?: {
            accessToken?: {
              value?: string;
            };
          };
        };
      };
    };

    const accessToken = parsedStorage.state?.auth?.authTokens?.accessToken?.value;
    if (!accessToken) {
      throw new Error("Access token is missing from proto-os-auth");
    }

    this.authAccessToken = accessToken;
  }

  clearAuthAccessToken() {
    this.authAccessToken = "";
  }

  hasAuthAccessToken() {
    return this.authAccessToken !== "";
  }

  async getState(): Promise<FirmwareState> {
    const response = await this.request.get("/api/v1/system");
    expect(response.ok()).toBeTruthy();

    const data = (await response.json()) as {
      "system-info": {
        sw_update_status: {
          status: string;
          current_version?: string;
          new_version?: string;
          previous_version?: string;
        };
      };
    };

    const updateStatus = data["system-info"].sw_update_status;

    return {
      status: updateStatus.status,
      currentVersion: updateStatus.current_version ?? "",
      newVersion: updateStatus.new_version ?? null,
      previousVersion: updateStatus.previous_version ?? null,
    };
  }

  async waitForStatus(expectedStatus: string, timeoutMs: number = FIRMWARE_STATUS_TIMEOUT_MS) {
    const deadline = Date.now() + timeoutMs;
    let lastState: FirmwareState | null = null;
    let lastError: unknown = null;

    while (Date.now() < deadline) {
      try {
        lastState = await this.getState();
        if (lastState.status === expectedStatus) {
          return lastState;
        }
        lastError = null;
      } catch (error: unknown) {
        lastError = error;
      }

      await sleep(FIRMWARE_STATUS_POLL_INTERVAL_MS);
    }

    throw new Error(
      `Timed out waiting for firmware status "${expectedStatus}". Last state: ${JSON.stringify(lastState)}. Last error: ${
        lastError instanceof Error ? lastError.message : String(lastError)
      }`,
    );
  }

  async waitForAnyStatus(expectedStatuses: string[], timeoutMs: number = FIRMWARE_STATUS_TIMEOUT_MS) {
    const deadline = Date.now() + timeoutMs;
    let lastState: FirmwareState | null = null;
    let lastError: unknown = null;

    while (Date.now() < deadline) {
      try {
        lastState = await this.getState();
        if (expectedStatuses.includes(lastState.status)) {
          return lastState;
        }
        lastError = null;
      } catch (error: unknown) {
        lastError = error;
      }

      await sleep(FIRMWARE_STATUS_POLL_INTERVAL_MS);
    }

    throw new Error(
      `Timed out waiting for firmware status in [${expectedStatuses.join(", ")}]. Last state: ${JSON.stringify(lastState)}. Last error: ${
        lastError instanceof Error ? lastError.message : String(lastError)
      }`,
    );
  }

  private async assertSafeDummyUploadTarget() {
    if (this.hasValidatedDummyUploadTarget) {
      return;
    }

    const response = await this.request.get("/api/v1/system");
    expect(response.ok()).toBeTruthy();

    const data = (await response.json()) as {
      "system-info": {
        cb_sn?: string;
        product_name?: string;
        manufacturer?: string;
        model?: string;
      };
    };

    const systemInfo = data["system-info"];
    const serialNumber = systemInfo.cb_sn ?? "";

    if (!serialNumber.startsWith(FAKE_PROTO_RIG_SERIAL_PREFIX)) {
      throw new Error(
        `Refusing to upload a dummy firmware bundle to non-simulator target "${serialNumber || "unknown"}" (${systemInfo.manufacturer ?? "unknown"} ${systemInfo.product_name ?? systemInfo.model ?? "unknown"}).`,
      );
    }

    this.hasValidatedDummyUploadTarget = true;
  }

  async uploadBundle() {
    if (!this.authAccessToken) {
      throw new Error("Firmware helper is missing an auth access token");
    }

    await this.assertSafeDummyUploadTarget();

    const response = await this.request.put("/api/v1/system/update", {
      headers: {
        Authorization: `Bearer ${this.authAccessToken}`,
      },
      multipart: {
        file: {
          name: "proto-os-test-update.swu",
          mimeType: "application/octet-stream",
          buffer: Buffer.from("fake firmware bundle for e2e"),
        },
      },
    });

    expect(response.status()).toBe(200);
  }

  private async startInstall() {
    if (!this.authAccessToken) {
      throw new Error("Firmware helper is missing an auth access token");
    }

    const response = await this.request.post("/api/v1/system/update", {
      headers: {
        Authorization: `Bearer ${this.authAccessToken}`,
      },
    });

    expect(response.status()).toBe(202);
  }

  private async rebootAfterUpdate() {
    if (!this.authAccessToken) {
      throw new Error("Firmware helper is missing an auth access token");
    }

    const response = await this.request.post("/api/v1/system/reboot", {
      headers: {
        Authorization: `Bearer ${this.authAccessToken}`,
      },
    });

    expect(response.status()).toBe(202);
  }

  async ensureCurrentState(): Promise<void> {
    const state = await this.getState();

    switch (state.status) {
      case "current":
        return;
      case "downloading":
        await this.waitForAnyStatus(["downloaded", "installing", "installed", "current"]);
        return this.ensureCurrentState();
      case "downloaded":
        await this.startInstall();
        await this.waitForStatus("installed");
        return this.ensureCurrentState();
      case "installing":
        await this.waitForStatus("installed");
        return this.ensureCurrentState();
      case "installed":
        await this.rebootAfterUpdate();
        await this.waitForStatus("current");
        return;
      default:
        throw new Error(`Unexpected firmware status during cleanup: ${state.status}`);
    }
  }
}
