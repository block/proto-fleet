import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import CT1FirmwareUpload from "./CT1FirmwareUpload";
import type { CT1Firmware, CT1FirmwareAttempt, CT1FirmwareProgress } from "@/protoOS/api/adapters/ct1Firmware";

const key = "proto-os:ct1-firmware:device:task";
const attempt: CT1FirmwareAttempt = {
  taskId: "task",
  boards: [{ mac: "02:00:00:00:00:01", owner: "controller", label: "Tank 1 · Module 1 · Board 1" }],
  version: "9.0.0",
  build: "prod",
};
const unreachable = "controller: Controller unavailable";
const setup = (saved: CT1FirmwareAttempt = attempt, progress: CT1FirmwareProgress = { boards: [], errors: [] }) => {
  const firmware = {
    prepare: vi.fn<CT1Firmware["prepare"]>(),
    install: vi.fn<CT1Firmware["install"]>(),
    progress: vi.fn<CT1Firmware["progress"]>().mockResolvedValue(progress),
    cancel: vi.fn<CT1Firmware["cancel"]>().mockRejectedValue(new Error(unreachable)),
  };
  localStorage.setItem(key, JSON.stringify(saved));
  render(<CT1FirmwareUpload firmware={firmware} deviceId="device" />);
  fireEvent.click(screen.getByRole("button", { name: "Hashboard update status" }));
  return firmware;
};

describe("CT1FirmwareUpload", () => {
  beforeEach(() => localStorage.clear());

  it("offers to stop tracking only after a cancel could not be confirmed, and asks before releasing", async () => {
    const firmware = setup();
    expect(screen.queryByRole("button", { name: "Stop tracking" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Cancel update" }));
    fireEvent.click(await screen.findByRole("button", { name: "Stop tracking" }));
    expect(firmware.cancel).toHaveBeenCalledOnce();
    expect(screen.getByRole("dialog", { name: "Stop tracking this update?" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Keep tracking" }));
    expect(screen.getByRole("dialog", { name: "Hashboard update status" })).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("not confirmed by every controller");
    expect(screen.getByRole("alert")).toHaveTextContent(unreachable);
    expect(localStorage.getItem(key)).not.toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Stop tracking" }));
    fireEvent.click(screen.getByRole("button", { name: "Stop tracking" }));
    expect(localStorage.getItem(key)).toBeNull();
    expect(screen.getByRole("dialog", { name: "Upload hashboard firmware" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Choose file" })).toBeEnabled();
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
