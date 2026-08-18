import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import userEvent from "@testing-library/user-event";

import FirmwareTransitionMinerDetails from "./FirmwareTransitionMinerDetails";
import type { FirmwareTransitionProgress } from "./rolloutTypes";

const progress: FirmwareTransitionProgress = {
  totalCount: 5,
  pendingCount: 1,
  updatingCount: 1,
  verifyingCount: 1,
  confirmedCount: 1,
  attentionCount: 1,
  members: [
    {
      deviceIdentifier: "miner-pending",
      manufacturer: "Proto",
      model: "Alpha",
      targetFirmwareVersion: "2.0.0",
      state: "pending",
    },
    {
      deviceIdentifier: "miner-updating",
      manufacturer: "Proto",
      model: "Alpha",
      latestObservedFirmwareVersion: "1.0.0",
      targetFirmwareVersion: "2.0.0",
      state: "updating",
    },
    {
      deviceIdentifier: "miner-verifying",
      manufacturer: "Proto",
      model: "Beta",
      latestObservedFirmwareVersion: "2.0.0",
      targetFirmwareVersion: "2.0.0",
      state: "verifying",
    },
    {
      deviceIdentifier: "miner-confirmed",
      manufacturer: "Proto",
      model: "Beta",
      latestObservedFirmwareVersion: "2.0.0",
      targetFirmwareVersion: "2.0.0",
      state: "confirmed",
    },
    {
      deviceIdentifier: "miner-attention",
      manufacturer: "Proto",
      model: "Gamma",
      latestObservedFirmwareVersion: "1.5.0",
      targetFirmwareVersion: "2.0.0",
      state: "needsAttention",
      lastError: "Firmware identity could not be confirmed",
    },
  ],
};

describe("FirmwareTransitionMinerDetails", () => {
  it("shows per-miner firmware and exact transition states without a duplicate summary", () => {
    render(<FirmwareTransitionMinerDetails progress={progress} />);

    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
    expect(screen.queryByText("1 of 5 confirmed")).not.toBeInTheDocument();
    expect(screen.queryByText("Firmware confirmation")).not.toBeInTheDocument();

    const table = within(screen.getByRole("table"));
    expect(table.getByRole("columnheader", { name: "Latest observed firmware" })).toBeInTheDocument();
    expect(table.getByRole("columnheader", { name: "Target firmware" })).toBeInTheDocument();
    expect(table.getByText("Updating firmware")).toBeInTheDocument();
    expect(table.getByText("Verifying firmware")).toBeInTheDocument();
    expect(table.getByText("Firmware identity could not be confirmed")).toBeInTheDocument();
  });

  it.each([
    ["pending", "Pending"],
    ["updating", "Updating firmware"],
    ["verifying", "Verifying firmware"],
    ["confirmed", "Confirmed"],
    ["needsAttention", "Needs attention"],
  ] as const)("displays %s as %s", (state, label) => {
    render(
      <FirmwareTransitionMinerDetails
        progress={{
          ...progress,
          members: [{ ...progress.members[0], state }],
        }}
      />,
    );

    expect(within(screen.getByRole("table")).getByText(label)).toBeInTheDocument();
  });

  it("renders an empty state without empty table chrome", () => {
    render(<FirmwareTransitionMinerDetails progress={{ ...progress, members: [] }} />);

    expect(screen.getByText("Miner details are unavailable.")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("keeps large transition sets bounded while every miner remains reachable", async () => {
    const user = userEvent.setup();
    const members = Array.from({ length: 101 }, (_, index) => ({
      ...progress.members[0],
      deviceIdentifier: `miner-${index + 1}`,
    }));
    render(<FirmwareTransitionMinerDetails progress={{ ...progress, members }} />);

    expect(screen.getByText("miner-100")).toBeInTheDocument();
    expect(screen.queryByText("miner-101")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Show more miners" }));
    expect(screen.getByText("miner-101")).toBeInTheDocument();
  });
});
