import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";
import PowerFeedActionDialog from "./PowerFeedActionDialog";
import { powerFeedFailure } from "./powerFeedOutcome";
import { PowerFeedAction } from "@/protoFleet/api/generated/containers/v1/containers_pb";

const mocks = vi.hoisted(() => ({ control: vi.fn(), results: vi.fn() }));
vi.mock("@/protoFleet/api/clients", () => ({
  containersClient: { controlModulePowerFeed: mocks.control, getEquipmentCommandResult: mocks.results },
}));

const finished = (errorMessage?: string) => ({
  result: {
    status: "finished",
    successCount: errorMessage ? 0 : 1,
    failureCount: errorMessage ? 1 : 0,
    deviceResults: errorMessage ? [{ errorMessage }] : [],
  },
});

const renderDialog = (action: "turn_off" | "cycle" | "turn_on" | "clear_override" = "cycle") => {
  const onClose = vi.fn();
  const onSettled = vi.fn();
  const { unmount } = render(
    <PowerFeedActionDialog
      moduleId={91n}
      deviceIdentifier="north"
      feedId="pdu-1-outlet-05"
      containerName="North CT1"
      tank={1}
      module={2}
      action={action}
      cycleHoldSeconds={30}
      onClose={onClose}
      onSettled={onSettled}
    />,
  );
  return { onClose, onSettled, unmount };
};

beforeEach(() => {
  vi.clearAllMocks();
  mocks.control.mockResolvedValue({ deviceIdentifier: "north", batchIdentifier: "batch-1" });
  mocks.results.mockResolvedValue(finished());
});

it("names the hardware and cycle duration, sends the confirmed feed once and reports the controller's acceptance", async () => {
  const { onSettled } = renderDialog("cycle");
  const dialog = screen.getByTestId("power-feed-action-dialog");
  expect(dialog).toHaveTextContent("Cycle feed pdu-1-outlet-05?");
  expect(dialog).toHaveTextContent("North CT1 · Tank 1 · Module 2");
  expect(dialog).toHaveTextContent("turns it back on after 30 seconds");
  expect(dialog).toHaveTextContent("It does not stop mining first.");

  const confirm = screen.getByRole("button", { name: "Cycle feed" });
  fireEvent.click(confirm);
  fireEvent.click(confirm);
  expect(await screen.findByRole("status")).toHaveTextContent("owns the restore");
  expect(mocks.control).toHaveBeenCalledExactlyOnceWith(
    { moduleId: 91n, deviceIdentifier: "north", action: PowerFeedAction.CYCLE, feedId: "pdu-1-outlet-05" },
    expect.anything(),
  );
  expect(mocks.results).toHaveBeenCalledWith(
    { deviceIdentifier: "north", batchIdentifier: "batch-1" },
    expect.anything(),
  );
  expect(onSettled).toHaveBeenCalledOnce();
  expect(screen.queryByRole("button", { name: "Cycle feed" })).toBeNull();
  expect(screen.getByRole("button", { name: "Close" })).toBeEnabled();
});

it("warns that turning a feed off does not stop mining", () => {
  renderDialog("turn_off");
  expect(screen.getByTestId("power-feed-action-dialog")).toHaveTextContent(
    "It does not stop mining first; stop the module's mining for a clean shutdown.",
  );
});

it("reports a controller refusal as not applied and offers no retry", async () => {
  mocks.results.mockResolvedValue(
    finished(
      "failed_precondition: container power feed: controller refused to turn off power feed pdu-1-outlet-05: interlock active",
    ),
  );
  const { onSettled } = renderDialog("turn_off");
  fireEvent.click(screen.getByRole("button", { name: "Turn off feed" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Not applied. Controller refused to turn off power feed pdu-1-outlet-05: interlock active",
  );
  expect(onSettled).toHaveBeenCalledOnce();
  expect(screen.queryByRole("button", { name: "Turn off feed" })).toBeNull();
});

it("reports an unconfirmed write as unknown without resending it", async () => {
  mocks.results.mockResolvedValue(finished("failed_precondition: module power feed outcome unconfirmed"));
  renderDialog("turn_on");
  fireEvent.click(screen.getByRole("button", { name: "Turn on feed" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Fleet could not confirm the result.");
  expect(mocks.control).toHaveBeenCalledOnce();
});

it("treats a request error that may have queued the command as unknown", async () => {
  mocks.control.mockRejectedValueOnce(new ConnectError("upstream reset", Code.Unavailable));
  renderDialog("clear_override");
  fireEvent.click(screen.getByRole("button", { name: "Clear override" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Fleet could not confirm the result.");
  expect(mocks.results).not.toHaveBeenCalled();
});

it("shows the server's reason when it refuses to queue the command", async () => {
  mocks.control.mockRejectedValueOnce(
    new ConnectError("A power feed command is already pending for this module.", Code.FailedPrecondition),
  );
  const { onSettled } = renderDialog("turn_on");
  fireEvent.click(screen.getByRole("button", { name: "Turn on feed" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Not applied. A power feed command is already pending for this module.",
  );
  expect(onSettled).toHaveBeenCalledOnce();
  expect(mocks.results).not.toHaveBeenCalled();
});

it("reports a refusal to queue as not applied even when its reason says unconfirmed", async () => {
  mocks.control.mockRejectedValueOnce(
    new ConnectError(
      "Module identity is unconfirmed. Refresh the module and check its controller connection before retrying.",
      Code.FailedPrecondition,
    ),
  );
  renderDialog("turn_off");
  fireEvent.click(screen.getByRole("button", { name: "Turn off feed" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Not applied. Module identity is unconfirmed.");
  expect(mocks.results).not.toHaveBeenCalled();
});

it("stays open until the controller's result arrives", async () => {
  let finish: (value: unknown) => void = () => {};
  mocks.results.mockReturnValue(new Promise((resolve) => (finish = resolve)));
  const { onClose, onSettled } = renderDialog("turn_off");
  fireEvent.click(screen.getByRole("button", { name: "Turn off feed" }));
  expect(await screen.findByRole("status")).toHaveTextContent("Waiting for the controller's result");
  expect(screen.getByRole("button", { name: "Close" })).toBeDisabled();
  fireEvent.keyDown(document, { key: "Escape" });
  expect(onClose).not.toHaveBeenCalled();
  expect(onSettled).not.toHaveBeenCalled();
  finish(finished("failed_precondition: container power feed: power feed pdu-1-outlet-05 is not on"));
  expect(await screen.findByRole("alert")).toHaveTextContent("Not applied. Power feed pdu-1-outlet-05 is not on");
  expect(screen.getByRole("button", { name: "Close" })).toBeEnabled();
  expect(onSettled).toHaveBeenCalledOnce();
});

it("keeps the dialog open while the request is in flight", async () => {
  let respond: (value: unknown) => void = () => {};
  mocks.control.mockReturnValue(new Promise((resolve) => (respond = resolve)));
  const { onClose } = renderDialog("turn_off");
  fireEvent.click(screen.getByRole("button", { name: "Turn off feed" }));
  expect(screen.getByRole("button", { name: "Sending…" })).toBeDisabled();
  fireEvent.keyDown(document, { key: "Escape" });
  expect(onClose).not.toHaveBeenCalled();
  respond({ deviceIdentifier: "north", batchIdentifier: "batch-1" });
  await waitFor(() => expect(screen.getByRole("button", { name: "Close" })).toBeEnabled());
});

it("re-reads the feed if the dialog unmounts before the result arrives", async () => {
  mocks.results.mockResolvedValue({ result: { status: "running" } });
  const { onSettled, unmount } = renderDialog("turn_off");
  fireEvent.click(screen.getByRole("button", { name: "Turn off feed" }));
  expect(await screen.findByRole("status")).toHaveTextContent("Waiting for the controller's result");
  expect(onSettled).not.toHaveBeenCalled();
  unmount();
  expect(onSettled).toHaveBeenCalledOnce();
});

it("does not re-read again on close once the result arrived, or when nothing was sent", async () => {
  const sent = renderDialog("turn_on");
  fireEvent.click(screen.getByRole("button", { name: "Turn on feed" }));
  await waitFor(() => expect(sent.onSettled).toHaveBeenCalledOnce());
  sent.unmount();
  expect(sent.onSettled).toHaveBeenCalledOnce();

  const unsent = renderDialog("turn_on");
  unsent.unmount();
  expect(unsent.onSettled).not.toHaveBeenCalled();
});

it("classifies tracked failures", () => {
  for (const message of [
    "Command outcome unconfirmed. Refresh before retrying.",
    "Equipment command failed; state unconfirmed.",
    "Interrupted by Fleet restart; device outcome may be unknown",
    "reaped: stuck in PROCESSING beyond timeout",
    "failed_precondition: module power feed outcome unconfirmed",
  ]) {
    expect(powerFeedFailure(new Error(message)), message).toEqual({ status: "unknown" });
  }
  expect(
    powerFeedFailure(new Error("failed_precondition: power feed command was not sent; refresh before retrying")),
  ).toEqual({
    status: "failed",
    message: "Power feed command was not sent; refresh before retrying",
  });
  expect(powerFeedFailure(new Error("internal: container power feed: controller unreachable"))).toEqual({
    status: "failed",
    message: "Controller unreachable",
  });
});
