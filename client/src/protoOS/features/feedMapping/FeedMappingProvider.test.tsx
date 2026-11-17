import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import { useFeedMappingState } from "./feedMappingContext";
import { FeedMappingProvider } from "./FeedMappingProvider";
import ContainerMappingSetup from "./MappingSetup";
import type { FeedMapping, MappingRun } from "@/protoOS/api/adapters/feedMapping";
import NoPoolsCallout from "@/protoOS/components/NoPoolsCallout";
import { useNoPoolsCalloutState } from "@/protoOS/components/NoPoolsCallout/useNoPoolsCalloutState";

const running: MappingRun = {
  id: "pool-test-run",
  status: "running",
  phase: "WAITING_OFFLINE",
  feeds: [],
  warnings: [],
};
const lostConnection = "This miner has lost connection to all mining pools.";
const inactivePools = [{ url: "stratum+tcp://pool.example:3333", status: "Idle" }];
const api = () =>
  ({
    assess: vi.fn().mockResolvedValue({
      mode: "manual",
      hardMappingAvailable: true,
      boards: [],
      configurationReasons: [],
      changes: [],
      historyIncomplete: false,
    }),
    active: vi.fn<FeedMapping["active"]>().mockResolvedValue(running),
    get: vi.fn<FeedMapping["get"]>().mockResolvedValue(running),
    plan: vi.fn<FeedMapping["plan"]>(),
    start: vi.fn<FeedMapping["start"]>(),
    cancel: vi.fn<FeedMapping["cancel"]>(),
    verify: vi.fn<FeedMapping["verify"]>(),
  }) satisfies FeedMapping;

function PoolStatus({ pools = inactivePools }: { pools?: typeof inactivePools }) {
  const state = useFeedMappingState();
  const callout = useNoPoolsCalloutState(pools, "/");
  return (
    <>
      <output aria-label="Mapping phase">{state?.run?.phase}</output>
      <button onClick={state?.refresh}>Read mapping status</button>
      {callout.shouldShowNoPoolsCallout ? <NoPoolsCallout arePoolsConfigured={callout.arePoolsConfigured} /> : null}
    </>
  );
}

beforeEach(() => localStorage.clear());

test.each(["STARTING", "ISOLATING", "WAITING_ONLINE", "WAITING_OFFLINE", "PERSISTING", "RESTORING", "CANCELING"])(
  "hides lost pool connections during %s",
  async (phase) => {
    const mapping = api();
    mapping.active.mockResolvedValue({ ...running, phase });
    render(
      <MemoryRouter>
        <FeedMappingProvider mapping={mapping} deviceId="ct1">
          <PoolStatus />
        </FeedMappingProvider>
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByLabelText("Mapping phase")).toHaveTextContent(phase));
    expect(screen.queryByText(lostConnection)).not.toBeInTheDocument();
  },
);

test("keeps the no-pools-configured warning during mapping", async () => {
  render(
    <MemoryRouter>
      <FeedMappingProvider mapping={api()} deviceId="ct1">
        <PoolStatus pools={[]} />
      </FeedMappingProvider>
    </MemoryRouter>,
  );
  await waitFor(() => expect(screen.getByLabelText("Mapping phase")).toHaveTextContent("WAITING_OFFLINE"));
  expect(screen.getByText("No mining pools configured.")).toBeInTheDocument();
});

test.each(["completed", "failed", "canceled"] as const)(
  "continues tracking after the mapping panel unmounts and restores pool warnings when %s",
  async (status) => {
    const mapping = api();
    const content = (showPanel: boolean) => (
      <MemoryRouter>
        <FeedMappingProvider mapping={mapping} deviceId="ct1">
          <PoolStatus />
          {showPanel ? <ContainerMappingSetup /> : null}
        </FeedMappingProvider>
      </MemoryRouter>
    );
    const view = render(content(true));
    await screen.findByText("Hashboard mapping is running");
    view.rerender(content(false));
    expect(screen.queryByText(lostConnection)).not.toBeInTheDocument();
    const finished = { ...running, status, phase: status.toUpperCase() };
    mapping.active.mockResolvedValue(null);
    mapping.get.mockResolvedValue(finished);
    fireEvent.click(screen.getByRole("button", { name: "Read mapping status" }));
    expect(await screen.findByText(lostConnection)).toBeInTheDocument();
    expect(screen.getByLabelText("Mapping phase")).toHaveTextContent(finished.phase);
  },
);

test("retains ordinary pool warnings without a mapping provider", () => {
  render(
    <MemoryRouter>
      <PoolStatus />
    </MemoryRouter>,
  );
  expect(screen.getByText(lostConnection)).toBeInTheDocument();
});

test("does not suppress pool warnings for an unconfirmed run status", async () => {
  const mapping = api();
  mapping.active.mockResolvedValue({ ...running, status: "unknown", phase: "UNKNOWN" });
  render(
    <MemoryRouter>
      <FeedMappingProvider mapping={mapping} deviceId="ct1">
        <PoolStatus />
      </FeedMappingProvider>
    </MemoryRouter>,
  );
  await waitFor(() => expect(screen.getByLabelText("Mapping phase")).toHaveTextContent("UNKNOWN"));
  expect(screen.getByText(lostConnection)).toBeInTheDocument();
});
