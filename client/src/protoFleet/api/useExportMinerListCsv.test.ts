import { act, renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { fleetManagementClient } from "./clients";
import useExportMinerListCsv from "./useExportMinerListCsv";

vi.mock("./clients", () => ({ fleetManagementClient: { exportMinerListCsv: vi.fn() } }));
vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({ handleAuthErrors: vi.fn() }),
  useTemperatureUnit: () => "C",
}));

it("never issues an unfiltered export when the site scope disables it", async () => {
  const { result, rerender } = renderHook(({ enabled }) => useExportMinerListCsv({ enabled }), {
    initialProps: { enabled: true },
  });
  rerender({ enabled: false });
  await act(async () => result.current.exportCsv());
  expect(fleetManagementClient.exportMinerListCsv).not.toHaveBeenCalled();
  expect(result.current.isExportingCsv).toBe(false);
});
