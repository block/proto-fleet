import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import CT1FirmwareUpload from "./CT1FirmwareUpload";
import type { CT1Firmware, CT1FirmwareAttempt } from "@/protoOS/api/adapters/ct1Firmware";

const key = "proto-os:ct1-firmware:device:task";
const attempt: CT1FirmwareAttempt = {
  taskId: "task",
  boards: [{ mac: "02:00:00:00:00:01", owner: "controller", label: "Tank 1 · Module 1 · Board 1" }],
  version: "9.0.0",
  build: "prod",
};
const unreachable = "controller: Controller unavailable";
const setup = () => {
  const firmware = {
    prepare: vi.fn<CT1Firmware["prepare"]>(),
    install: vi.fn<CT1Firmware["install"]>(),
    progress: vi.fn<CT1Firmware["progress"]>().mockResolvedValue({ complete: false, boards: [], errors: [] }),
    cancel: vi.fn<CT1Firmware["cancel"]>().mockRejectedValue(new Error(unreachable)),
  };
  localStorage.setItem(key, JSON.stringify(attempt));
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
});
