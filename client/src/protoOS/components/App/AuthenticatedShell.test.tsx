import { render } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";

import AuthenticatedShell from "./AuthenticatedShell";

const mocks = vi.hoisted(() => ({
  adapterId: "mdk-v2" as string | undefined,
  hashboardStatus: vi.fn(),
  hashboardSerials: vi.fn(() => ["HB-1", "HB-2"]),
  checkFirmwareUpdate: vi.fn(() => Promise.resolve()),
}));

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ resolution: { adapterId: mocks.adapterId } }),
}));
vi.mock("@/protoOS/api/hooks/useHashboardStatus", () => ({ useHashboardStatus: mocks.hashboardStatus }));
vi.mock("@/protoOS/store", () => ({ useHashboardSerials: mocks.hashboardSerials }));
vi.mock("@/protoOS/api/hooks/useNetworkInfo", () => ({ useNetworkInfo: vi.fn() }));
vi.mock("@/protoOS/api/hooks/useErrors", () => ({ useErrors: vi.fn() }));
vi.mock("@/protoOS/api/hooks/usePoolsInfo", () => ({ usePoolsInfo: vi.fn() }));
vi.mock("@/protoOS/api/hooks/useFirmwareUpdate", () => ({
  useFirmwareUpdate: () => ({ checkFirmwareUpdate: mocks.checkFirmwareUpdate }),
}));

beforeEach(() => vi.clearAllMocks());

test.each(["mdk-v1", "mdk-v2", undefined])("skips eager ASIC loading for adapter %s", (adapterId) => {
  mocks.adapterId = adapterId;
  render(<AuthenticatedShell reloadSystemInfo={vi.fn()} />);
  expect(mocks.hashboardSerials).not.toHaveBeenCalled();
  expect(mocks.hashboardStatus).not.toHaveBeenCalled();
});
