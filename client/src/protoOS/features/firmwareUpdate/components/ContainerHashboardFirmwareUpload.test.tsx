import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ContainerHashboardFirmwareUpload from "./ContainerHashboardFirmwareUpload";
import {
  type HashboardFirmware,
  type HashboardFirmwareAttempt,
  HashboardFirmwareCancelError,
  type HashboardFirmwareProgress,
} from "@/protoOS/api/adapters/hashboardFirmware";

const key = "proto-os:hashboard-firmware:device:task";
const attempt: HashboardFirmwareAttempt = {
  taskId: "task",
  boards: [{ mac: "02:00:00:00:00:01", owner: "controller", label: "Tank 1 · Module 1 · Board 1" }],
  version: "9.0.0",
  build: "prod",
};
const unreachable = "controller: Controller unavailable";
const setup = (
  saved: HashboardFirmwareAttempt = attempt,
  progress: HashboardFirmwareProgress = { boards: [], errors: [] },
) => {
  const firmware = {
    readImage: vi.fn<HashboardFirmware["readImage"]>(),
    prepare: vi.fn<HashboardFirmware["prepare"]>(),
    install: vi.fn<HashboardFirmware["install"]>(),
    progress: vi.fn<HashboardFirmware["progress"]>().mockResolvedValue(progress),
    cancel: vi.fn<HashboardFirmware["cancel"]>().mockRejectedValue(new Error(unreachable)),
  };
  localStorage.setItem(key, JSON.stringify(saved));
  render(<ContainerHashboardFirmwareUpload firmware={firmware} deviceId="device" />);
  fireEvent.click(screen.getByRole("button", { name: "Hashboard update status" }));
  return firmware;
};
const statusDialog = () => screen.getByRole("dialog", { name: "Hashboard update status" });
const question = () => screen.getByRole("dialog", { name: "Stop tracking this update?" });

describe("ContainerHashboardFirmwareUpload", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.restoreAllMocks());

  it("offers to stop tracking only after a cancel could not be confirmed, and asks before releasing", async () => {
    const firmware = setup();
    expect(screen.queryByRole("button", { name: "Stop tracking" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Cancel update" }));
    fireEvent.click(await screen.findByRole("button", { name: "Stop tracking" }));
    expect(firmware.cancel).toHaveBeenCalledOnce();
    // The question is asked over the status dialog, which stays out of reach until it is answered.
    expect(question()).toBeInTheDocument();
    expect(statusDialog()).toHaveAttribute("inert");

    fireEvent.click(within(question()).getByRole("button", { name: "Keep tracking" }));
    expect(screen.queryByRole("dialog", { name: "Stop tracking this update?" })).not.toBeInTheDocument();
    expect(statusDialog()).not.toHaveAttribute("inert");
    expect(screen.getByRole("status")).toHaveTextContent("not confirmed by every controller");
    expect(screen.getByRole("alert")).toHaveTextContent(unreachable);
    expect(localStorage.getItem(key)).not.toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Stop tracking" }));
    fireEvent.click(within(question()).getByRole("button", { name: "Stop tracking" }));
    expect(localStorage.getItem(key)).toBeNull();
    expect(screen.getByRole("dialog", { name: "Upload hashboard firmware" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Choose file" })).toBeEnabled();
  });

  it("opens the stop-tracking question with focus on keeping the record, and returns focus when declined", () => {
    // jsdom lays nothing out; the shared Dialog only focuses elements that have a box.
    vi.spyOn(HTMLElement.prototype, "getClientRects").mockReturnValue(
      Object.assign([new DOMRect()], { item: () => null }),
    );
    setup({ ...attempt, cancel: "unconfirmed" });
    const opener = screen.getByRole("button", { name: "Stop tracking" });
    opener.focus();
    fireEvent.click(opener);
    expect(within(question()).getByRole("button", { name: "Keep tracking" })).toHaveFocus();
    // A second press of the same key therefore keeps the record instead of releasing it.
    fireEvent.click(document.activeElement!);
    expect(screen.queryByRole("dialog", { name: "Stop tracking this update?" })).not.toBeInTheDocument();
    expect(localStorage.getItem(key)).not.toBeNull();
    expect(opener).toHaveFocus();
  });

  it("clears tracking without asking once every owner confirms the cancel", async () => {
    const firmware = setup();
    firmware.cancel.mockResolvedValue({ pending: false, finished: false, absent: [] });
    fireEvent.click(screen.getByRole("button", { name: "Cancel update" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Choose file" })).toBeEnabled());
    expect(screen.queryByRole("button", { name: "Stop tracking" })).not.toBeInTheDocument();
    expect(localStorage.getItem(key)).toBeNull();
  });

  it.each([
    ["completed", "Update finished"],
    ["completedWithErrors", "Update finished with errors"],
    ["canceled", "Update canceled"],
    ["interrupted", "Update interrupted"],
    ["failed", "Update failed"],
  ] as const)("states how an update that ended as %s ended", async (outcome, text) => {
    setup(attempt, { outcome, boards: [{ ...attempt.boards[0], status: "flashing", percent: 52 }], errors: [] });
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent(new RegExp(`^${text}$`)));
    expect(screen.getByRole("listitem")).toHaveTextContent("flashing · 52%");
    expect(screen.getByRole("button", { name: "Done" })).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Cancel update" })).not.toBeInTheDocument();
  });

  it("lets the operator dismiss an ended update whose controller never reported one of its boards", async () => {
    const missing = `${attempt.boards[0].label}: missing from update status`;
    setup(attempt, {
      outcome: "completedWithErrors",
      boards: [{ ...attempt.boards[0], status: "Status unavailable" }],
      errors: [missing],
    });
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("Update finished with errors"));
    expect(screen.getByRole("alert")).toHaveTextContent(missing);
    expect(screen.getByRole("listitem")).toHaveTextContent("Status unavailable");
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(localStorage.getItem(key)).toBeNull();
    expect(screen.getByRole("button", { name: "Upload hashboard firmware" })).toBeInTheDocument();
  });

  it("shows a saved unconfirmed cancel as unconfirmed after a reload, with the release on offer", () => {
    const firmware = setup({ ...attempt, cancel: "unconfirmed" });
    expect(screen.getByRole("status")).toHaveTextContent("not confirmed by every controller");
    expect(screen.getByRole("button", { name: "Retry cancel" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Stop tracking" })).toBeEnabled();
    expect(firmware.cancel).not.toHaveBeenCalled();
  });

  it("shows a saved accepted cancel as still stopping, without the release", () => {
    setup({ ...attempt, cancel: "requested" });
    expect(screen.getByRole("status")).toHaveTextContent("Cancel requested. Waiting for hashboards to stop");
    expect(screen.getByRole("button", { name: "Retry cancel" })).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Stop tracking" })).not.toBeInTheDocument();
  });

  it("shows one line when the status poll and a failed cancel report the same controller error", async () => {
    setup(attempt, { boards: [{ ...attempt.boards[0], status: "Status unavailable" }], errors: [unreachable] });
    await screen.findByRole("listitem");
    fireEvent.click(screen.getByRole("button", { name: "Cancel update" }));
    await screen.findByRole("button", { name: "Stop tracking" });
    const alerts = screen.getAllByRole("alert");
    expect(alerts.map((alert) => alert.textContent)).toEqual([unreachable]);
    // Messages come before the board list, so a long list cannot push them out of view.
    expect(alerts[0].compareDocumentPosition(screen.getByRole("list")) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("shows each controller's error once when the status poll and a cancel both fail on two controllers", async () => {
    const lines = ["controller-a: Controller unavailable", "controller-b: Controller unavailable"];
    const firmware = setup(attempt, { boards: [], errors: lines });
    firmware.cancel.mockRejectedValue(new HashboardFirmwareCancelError(lines));
    await screen.findAllByRole("alert");
    fireEvent.click(screen.getByRole("button", { name: "Cancel update" }));
    await screen.findByRole("button", { name: "Stop tracking" });
    expect(screen.getAllByRole("alert").map((alert) => alert.textContent)).toEqual(lines);
  });

  it("keeps distinct messages from the status poll and a failed cancel", async () => {
    const firmware = setup(attempt, { boards: [], errors: ["controller: Update not found."] });
    firmware.cancel.mockRejectedValue(new Error("controller: internal error"));
    await screen.findByRole("alert");
    fireEvent.click(screen.getByRole("button", { name: "Cancel update" }));
    await screen.findByRole("button", { name: "Stop tracking" });
    expect(screen.getAllByRole("alert").map((alert) => alert.textContent)).toEqual([
      "controller: Update not found.",
      "controller: internal error",
    ]);
  });
});
