import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ResolutionSectionContent } from "./ResolutionSection";

describe("ResolutionSectionContent", () => {
  it("hides an inapplicable repair location", () => {
    render(
      <ResolutionSectionContent resolution="Deferred" repairLocation="unknown" partsUsed={[]} resolutionNote="" />,
    );

    expect(screen.getByText("Deferred")).toBeInTheDocument();
    expect(screen.queryByText("Repair location")).not.toBeInTheDocument();
    expect(screen.queryByText("unknown")).not.toBeInTheDocument();
  });

  it("shows a recorded repair location", () => {
    render(
      <ResolutionSectionContent resolution="Repaired" repairLocation="On-site" partsUsed={[]} resolutionNote="" />,
    );

    expect(screen.getByText("Repair location")).toBeInTheDocument();
    expect(screen.getByText("On-site")).toBeInTheDocument();
  });

  it("shows the explicit resolution note", () => {
    render(
      <ResolutionSectionContent
        resolution="Repaired"
        repairLocation="unknown"
        partsUsed={[]}
        resolutionNote="Equipment repaired and verified"
      />,
    );

    expect(screen.getByRole("rowheader", { name: "Resolution note" })).toBeInTheDocument();
    expect(screen.getByText("Equipment repaired and verified")).toBeInTheDocument();
  });
});
