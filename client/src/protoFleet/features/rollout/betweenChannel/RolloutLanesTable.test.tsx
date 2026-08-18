import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import RolloutLanesTable, { type LaneTableRow } from "./RolloutLanesTable";

const rows: LaneTableRow[] = [
  {
    id: "lane-1",
    lane: {
      id: "lane-1",
      label: "Stable production",
      description: "Production firmware",
      currentChannelId: 41n,
      currentReleaseSetId: 7n,
      revision: 2n,
      channels: [],
      memberCount: 12,
      memberIdentifiers: [],
      currentReleaseTargets: [
        {
          firmwareFileId: "alpha-1",
          targetManufacturer: "Proto",
          targetModel: "Alpha",
          firmwareVersion: "1.0.0",
          sha256: "a",
        },
      ],
    },
  },
];

describe("RolloutLanesTable", () => {
  it("shows stable lane release and membership to read-only operators", () => {
    render(<RolloutLanesTable rows={rows} canStart={false} onStart={vi.fn()} onView={vi.fn()} />);

    expect(screen.getByText("Stable production")).toBeInTheDocument();
    expect(screen.getByText("Alpha 1.0.0")).toBeInTheDocument();
    expect(screen.getByText("12")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /start rollout/i })).not.toBeInTheDocument();
  });

  it("shows lane start only to managers", () => {
    render(<RolloutLanesTable rows={rows} canStart onStart={vi.fn()} onView={vi.fn()} />);

    expect(screen.getByRole("button", { name: "Start rollout for Stable production" })).toBeEnabled();
  });
});
