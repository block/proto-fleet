import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import { FeedMappingProvider } from "./FeedMappingProvider";
import ContainerMappingSetup from "./MappingSetup";
import type { FeedMapping, MappingAssessment, MappingRun } from "@/protoOS/api/adapters/feedMapping";

function MappingSetup(props: Omit<Parameters<typeof FeedMappingProvider>[0], "children">) {
  return (
    <FeedMappingProvider {...props}>
      <ContainerMappingSetup />
    </FeedMappingProvider>
  );
}

const assessment: MappingAssessment = {
  mode: "manual",
  hardMappingAvailable: true,
  configurationReasons: [],
  changes: [],
  historyIncomplete: false,
  boards: [
    {
      mac: "aa:bb:cc:00:00:01",
      tank: 0,
      module: 0,
      unlocated: true,
      conflict: false,
      status: "unverified",
      reasons: [],
      coverage: [],
    },
  ],
};
const run: MappingRun = {
  id: "run-1",
  status: "running",
  phase: "WAITING_ONLINE",
  feeds: [{ id: "feed-1", boards: 0, complete: false, timedOut: false }],
  warnings: [],
};
const api = () =>
  ({
    assess: vi.fn().mockResolvedValue(assessment),
    active: vi.fn().mockResolvedValue(null),
    get: vi.fn().mockResolvedValue(run),
    plan: vi.fn().mockResolvedValue({ ...run, status: "planned" }),
    start: vi.fn().mockResolvedValue(run),
    cancel: vi.fn().mockResolvedValue({ ...run, status: "canceled" }),
    verify: vi.fn().mockResolvedValue(undefined),
  }) satisfies FeedMapping;
beforeEach(() => localStorage.clear());

const open = async () => {
  fireEvent.click(await screen.findByRole("button", { name: "Refresh mapping" }));
  return screen.findByRole("dialog", { name: "Hashboard mapping" });
};

test("explains the power cycle and starts mapping once after confirmation", async () => {
  const mapping = api();
  render(<MappingSetup mapping={mapping} deviceId="ct1" />);
  expect(await screen.findByText("1 hashboard has no assigned location.")).toBeInTheDocument();
  const dialog = await open();
  expect(dialog).toHaveTextContent("changes power usage and reduces hashrate");
  expect(dialog).toHaveTextContent("Some actions will not be available until mapping is complete");
  expect(mapping.start).not.toHaveBeenCalled();
  const start = within(dialog).getByRole("button", { name: "Refresh mapping" });
  fireEvent.click(start);
  fireEvent.click(start);
  await waitFor(() => expect(mapping.start).toHaveBeenCalledTimes(1));
  expect(mapping.start.mock.calls[0][0]).toEqual(["feed-1"]);
  expect(await screen.findByText("Hashboard mapping is running")).toBeInTheDocument();
});

test("recovers an existing run without starting another", async () => {
  const mapping = api();
  mapping.active.mockResolvedValue(run);
  render(<MappingSetup mapping={mapping} deviceId="ct1" />);
  expect(await screen.findByText("Hashboard mapping is running")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "View progress" }));
  expect(screen.queryByRole("button", { name: "Refresh mapping" })).not.toBeInTheDocument();
  expect(mapping.start).not.toHaveBeenCalled();
});

test("blocks hard mapping in observation mode and explains configuration correction", async () => {
  const mapping = api();
  mapping.assess.mockResolvedValue({ ...assessment, mode: "observe" });
  render(<MappingSetup mapping={mapping} deviceId="ct1" />);
  const dialog = await open();
  expect(dialog).toHaveTextContent("Observation mode");
  expect(within(dialog).queryByRole("button", { name: "Refresh mapping" })).not.toBeInTheDocument();
});

test("retains an unknown start across reloads, even if no active run is found", async () => {
  const mapping = api();
  mapping.start.mockRejectedValue(new Error("Connection lost"));
  const view = render(<MappingSetup mapping={mapping} deviceId="ct1" />);
  const dialog = await open();
  fireEvent.click(within(dialog).getByRole("button", { name: "Refresh mapping" }));
  await screen.findByText("Mapping outcome is unknown");
  view.unmount();
  render(<MappingSetup mapping={mapping} deviceId="ct1" />);
  expect(await screen.findByText("Mapping outcome is unknown")).toBeInTheDocument();
  const recovered = await open();
  expect(within(recovered).queryByRole("button", { name: "Refresh mapping" })).not.toBeInTheDocument();
  expect(mapping.start).toHaveBeenCalledTimes(1);
});

test("does not clear missing locations merely because a run completed", async () => {
  const mapping = api();
  mapping.active.mockResolvedValue({ ...run, status: "completed" });
  render(<MappingSetup mapping={mapping} deviceId="ct1" />);
  expect(await screen.findByText("1 hashboard has no assigned location.")).toBeInTheDocument();
  await act(async () => {});
  expect(screen.getByText("Hashboard mapping needs attention")).toBeInTheDocument();
});

test("a failed active-run read blocks new writes without hiding unlocated boards", async () => {
  const mapping = api();
  mapping.active.mockRejectedValue(new Error("Controller unavailable"));
  render(<MappingSetup mapping={mapping} deviceId="ct1" />);
  const dialog = await open();
  expect(dialog).toHaveTextContent("Controller unavailable");
  expect(within(dialog).getByRole("button", { name: "Refresh mapping" })).toBeDisabled();
});

test("queues wiring verification only after confirmation and only for the displayed boards", async () => {
  const mapping = api();
  mapping.assess.mockResolvedValue({ ...assessment, mode: "switch_ports" });
  render(<MappingSetup mapping={mapping} deviceId="ct1" />);
  const dialog = await open();
  fireEvent.click(within(dialog).getByRole("button", { name: "Verify hashboards" }));
  const confirm = await screen.findByRole("dialog", { name: "Verify hashboard wiring?" });
  expect(confirm).toHaveTextContent("briefly stop and restart");
  expect(mapping.verify).not.toHaveBeenCalled();
  fireEvent.click(within(confirm).getByRole("button", { name: "Queue verification" }));
  await waitFor(() => expect(mapping.verify).toHaveBeenCalledTimes(1));
  expect(mapping.verify.mock.calls[0][0]).toEqual(["aa:bb:cc:00:00:01"]);
});

test("does not send Start if the device view unmounts during the preflight read", async () => {
  const mapping = api();
  const view = render(<MappingSetup mapping={mapping} deviceId="ct1" />);
  const dialog = await open();
  let resolve!: (value: MappingAssessment) => void;
  mapping.assess.mockImplementation(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  fireEvent.click(within(dialog).getByRole("button", { name: "Refresh mapping" }));
  await waitFor(() => expect(resolve).toBeDefined());
  view.unmount();
  await act(async () => resolve(assessment));
  expect(mapping.start).not.toHaveBeenCalled();
});

test("cancel uses the recovered run ID", async () => {
  const mapping = api();
  mapping.active.mockResolvedValue(run);
  render(<MappingSetup mapping={mapping} deviceId="ct1" />);
  fireEvent.click(await screen.findByRole("button", { name: "View progress" }));
  fireEvent.click(screen.getByRole("button", { name: "Cancel mapping" }));
  await waitFor(() => expect(mapping.cancel).toHaveBeenCalledTimes(1));
  expect(mapping.cancel.mock.calls[0][0]).toBe("run-1");
});

test("refreshes hardware and automatically hides a successful result with a healthy assessment", async () => {
  const mapping = api();
  const finished = { ...run, status: "completed" as const };
  mapping.active.mockResolvedValueOnce(finished).mockResolvedValue(null);
  mapping.get.mockResolvedValue(finished);
  mapping.assess.mockResolvedValue({ ...assessment, boards: [] });
  const onSettled = vi.fn().mockResolvedValue(undefined);
  render(<MappingSetup mapping={mapping} deviceId="ct1" onSettled={onSettled} />);
  await waitFor(() => expect(onSettled).toHaveBeenCalledTimes(1));
  await waitFor(() => expect(screen.queryByText("Hashboard mapping finished")).not.toBeInTheDocument());
  expect(screen.queryByRole("button", { name: "Refresh mapping" })).not.toBeInTheDocument();
  expect(localStorage.getItem("proto-os:feed-mapping:ct1")).toBeNull();
});

test("a second uncertain start cannot be dismissed as the previous completed run", async () => {
  const mapping = api();
  const finished = { ...run, status: "completed" as const };
  mapping.active.mockResolvedValueOnce(finished).mockResolvedValue(null);
  mapping.get.mockResolvedValue(finished);
  mapping.start.mockRejectedValue(new Error("Start response lost"));
  render(<MappingSetup mapping={mapping} deviceId="ct1" />);
  const dialog = await open();
  fireEvent.click(within(dialog).getByRole("button", { name: "Refresh mapping" }));
  expect(await screen.findByText("Mapping outcome is unknown")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Dismiss run result" })).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Clear saved attempt" })).toBeInTheDocument();
});

test("a wiring-only controller can verify without a hard mapping manager", async () => {
  const mapping = api();
  mapping.assess.mockResolvedValue({ ...assessment, mode: "switch_ports", hardMappingAvailable: false });
  mapping.active.mockRejectedValue(new Error("Feed mapping unavailable"));
  render(<MappingSetup mapping={mapping} deviceId="ct1" />);
  const dialog = await open();
  expect(within(dialog).getByRole("button", { name: "Verify hashboards" })).toBeEnabled();
  expect(within(dialog).queryByRole("button", { name: "Refresh mapping" })).not.toBeInTheDocument();
});

for (const [phase, copy] of [
  ["ISOLATING", "Turning feeds off before mapping…"],
  ["WAITING_ONLINE", "Waiting for hashboards to come online…"],
  ["WAITING_OFFLINE", "Confirming hashboards are offline…"],
  ["PERSISTING", "Saving hashboard locations…"],
  ["RESTORING", "Restoring power to all feeds…"],
]) {
  test(`shows a spinner and progress for ${phase}`, async () => {
    const mapping = api();
    mapping.active.mockResolvedValue({ ...run, phase });
    render(<MappingSetup mapping={mapping} deviceId="ct1" />);
    fireEvent.click(await screen.findByRole("button", { name: "View progress" }));
    expect(screen.getByTestId("mapping-progress-spinner")).toBeInTheDocument();
    expect(screen.getByText(copy)).toBeInTheDocument();
  });
}
