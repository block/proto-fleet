import { act, render } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import ContainerFeedMappingProvider from "./FeedMappingProvider";
import { toCanonicalContainer, toCanonicalSystemInfo } from "@/protoOS/api/adapters/mdkV2/normalize";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import useMinerStore from "@/protoOS/store/useMinerStore";

const mocks = vi.hoisted(() => ({
  getHardware: vi.fn(),
  settled: undefined as undefined | ((signal: AbortSignal) => Promise<void>),
}));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ adapter: { getHardware: mocks.getHardware, feedMapping: {} }, isFleetHosted: false }),
}));
vi.mock("./useFeedMapping", () => ({
  useFeedMapping: (_mapping: unknown, _id: string, settled: typeof mocks.settled) => {
    mocks.settled = settled;
    return {};
  },
}));
const snapshot = (located: boolean) =>
  toCanonicalContainer(
    {
      containerMap: {
        tanks: [
          {
            id: located ? 1 : 0,
            modules: [
              {
                id: located ? 1 : 0,
                slots: [
                  {
                    hashboard: {
                      serial: "board",
                      location: { hashboardPosition: located ? 1 : 0 },
                      health: { timestamp: new Date().toISOString(), hashrate: 2 },
                    },
                  },
                ],
              },
            ],
          },
        ],
      },
    },
    {},
    {},
    "ct1",
  );

beforeEach(() => {
  useMinerStore.getState().resetDeviceData();
  useMinerStore.getState().systemInfo.setSystemInfo(toCanonicalSystemInfo({}));
  mocks.getHardware.mockReset();
});

it("does not let a poll started before mapping restore the old location", async () => {
  const initial = snapshot(false);
  const store = useMinerStore.getState();
  store.hardware.replaceHardware(initial.hardware);
  const oldPoll = beginStoreWrite("telemetry:container");
  render(
    <ContainerFeedMappingProvider>
      <div />
    </ContainerFeedMappingProvider>,
  );
  mocks.getHardware.mockResolvedValue(snapshot(true));
  await act(() => mocks.settled!(new AbortController().signal));
  expect(oldPoll()).toBe(true); // Different resource key: the entity gate must reject it.
  act(() => store.telemetry.mergeTelemetry(initial.telemetry, oldPoll.requestId));
  expect([...useMinerStore.getState().hardware.topology!.hashboards.values()][0].moduleId).toBeDefined();
});
