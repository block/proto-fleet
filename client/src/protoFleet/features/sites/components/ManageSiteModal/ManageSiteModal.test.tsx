import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import ManageSiteModal from "./ManageSiteModal";
import { SiteSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { emptySiteFormValues, type SiteFormValues } from "@/protoFleet/api/sites";

vi.mock("@/protoFleet/api/buildings", () => ({
  useBuildings: () => ({
    listBuildingsBySite: vi.fn().mockResolvedValue(undefined),
    listAllBuildings: vi.fn(),
    getBuilding: vi.fn(),
  }),
}));

const draft = (overrides: Partial<SiteFormValues> = {}): SiteFormValues => ({
  ...emptySiteFormValues(),
  name: "North DC",
  ...overrides,
});

describe("ManageSiteModal", () => {
  it("invokes onSave and closes when the save reports closeOnSuccess", async () => {
    const onSave = vi.fn().mockResolvedValue({
      canonicalNetworkConfig: "10.0.0.0/24",
      warnings: [],
      closeOnSuccess: true,
    });
    const onDismiss = vi.fn();
    const onNetworkConfigChange = vi.fn();

    render(
      <ManageSiteModal
        open
        mode="create"
        draft={draft({ networkConfig: "10.0.0.0/24" })}
        onSave={onSave}
        onEditDetails={() => undefined}
        onNetworkConfigChange={onNetworkConfigChange}
        onDismiss={onDismiss}
      />,
    );

    // FullScreenTwoPaneModal renders the button twice (laptop + mobile);
    // click the first instance.
    fireEvent.click(screen.getAllByTestId("manage-site-modal-save")[0]);

    await waitFor(() => expect(onSave).toHaveBeenCalled());
    await waitFor(() => expect(onDismiss).toHaveBeenCalled());
  });

  it("renders a warning Callout and replaces the textarea with the canonical value", async () => {
    const onSave = vi.fn().mockResolvedValue({
      canonicalNetworkConfig: "10.0.0.0/24\n",
      warnings: ["overlaps with site East DC"],
      closeOnSuccess: false,
    });
    const onDismiss = vi.fn();
    const onNetworkConfigChange = vi.fn();

    render(
      <ManageSiteModal
        open
        mode="create"
        draft={draft({ networkConfig: "10.0.0.0/24" })}
        onSave={onSave}
        onEditDetails={() => undefined}
        onNetworkConfigChange={onNetworkConfigChange}
        onDismiss={onDismiss}
      />,
    );

    // FullScreenTwoPaneModal renders the button twice (laptop + mobile);
    // click the first instance.
    fireEvent.click(screen.getAllByTestId("manage-site-modal-save")[0]);

    await waitFor(() => expect(screen.getByTestId("manage-site-modal-warnings")).toBeInTheDocument());
    expect(onNetworkConfigChange).toHaveBeenCalledWith("10.0.0.0/24\n");
    expect(onDismiss).not.toHaveBeenCalled();
  });

  it("Edit details fires the parent callback", () => {
    const onEditDetails = vi.fn();
    render(
      <ManageSiteModal
        open
        mode="create"
        draft={draft()}
        onSave={() => Promise.resolve(null)}
        onEditDetails={onEditDetails}
        onNetworkConfigChange={() => undefined}
        onDismiss={() => undefined}
      />,
    );

    fireEvent.click(screen.getAllByTestId("manage-site-modal-edit-details")[0]);
    expect(onEditDetails).toHaveBeenCalled();
  });

  it("shows comma-separated meta on each corner of the preview", () => {
    const site = create(SiteSchema, {
      id: 7n,
      name: "East DC",
      locationCity: "Boston",
      locationState: "MA",
      powerCapacityMw: 5,
    });
    render(
      <ManageSiteModal
        open
        mode="edit"
        site={site}
        draft={draft({
          name: "East DC",
          locationCity: "Boston",
          locationState: "MA",
          powerCapacityMw: 5,
        })}
        onSave={() => Promise.resolve(null)}
        onEditDetails={() => undefined}
        onNetworkConfigChange={() => undefined}
        onDismiss={() => undefined}
      />,
    );
    expect(screen.getByText("East DC, Boston, MA")).toBeInTheDocument();
    expect(screen.getByText("5 MW, 0 buildings")).toBeInTheDocument();
  });
});
