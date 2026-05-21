import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import SiteDeleteDialog from "./SiteDeleteDialog";
import { SiteSchema, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";

const makeSite = (overrides: { deviceCount?: bigint; rackCount?: bigint; buildingCount?: bigint } = {}) =>
  create(SiteWithCountsSchema, {
    site: create(SiteSchema, { id: 1n, name: "North DC" }),
    deviceCount: overrides.deviceCount ?? 0n,
    rackCount: overrides.rackCount ?? 0n,
    buildingCount: overrides.buildingCount ?? 0n,
  });

describe("SiteDeleteDialog", () => {
  it("renders cascade copy when any count is non-zero", () => {
    render(
      <SiteDeleteDialog
        open
        site={makeSite({ deviceCount: 7n, rackCount: 2n, buildingCount: 3n })}
        onConfirm={() => undefined}
        onDismiss={() => undefined}
      />,
    );
    expect(
      screen.getByText(
        "Deleting will unassign 7 miners, 2 racks, and 3 buildings. They will be removed from this site.",
      ),
    ).toBeInTheDocument();
  });

  it("collapses to a bare confirm when all counts are zero", () => {
    render(<SiteDeleteDialog open site={makeSite()} onConfirm={() => undefined} onDismiss={() => undefined} />);
    expect(screen.getByText("Are you sure you want to delete this site?")).toBeInTheDocument();
  });

  it("singularizes the cascade rows when each count is exactly 1", () => {
    render(
      <SiteDeleteDialog
        open
        site={makeSite({ deviceCount: 1n, rackCount: 1n, buildingCount: 1n })}
        onConfirm={() => undefined}
        onDismiss={() => undefined}
      />,
    );
    expect(
      screen.getByText("Deleting will unassign 1 miner, 1 rack, and 1 building. They will be removed from this site."),
    ).toBeInTheDocument();
  });

  it("invokes onConfirm when Delete site is clicked", () => {
    const onConfirm = vi.fn();
    render(
      <SiteDeleteDialog open site={makeSite({ deviceCount: 1n })} onConfirm={onConfirm} onDismiss={() => undefined} />,
    );
    fireEvent.click(screen.getByTestId("site-delete-dialog-confirm"));
    expect(onConfirm).toHaveBeenCalled();
  });
});
