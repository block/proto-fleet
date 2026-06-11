import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import BuildingSettingsModal from "./BuildingSettingsModal";
import { type BuildingFormValues, emptyBuildingFormValues } from "@/protoFleet/api/buildings";
import { SiteSchema, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";

const baseValues = (): BuildingFormValues => emptyBuildingFormValues();

const makeSites = () => [
  create(SiteWithCountsSchema, { site: create(SiteSchema, { id: 7n, name: "North DC" }) }),
  create(SiteWithCountsSchema, { site: create(SiteSchema, { id: 9n, name: "South DC" }) }),
];

describe("BuildingSettingsModal — create mode", () => {
  it("disables Save until both a site and a name are entered", () => {
    render(
      <BuildingSettingsModal
        open
        mode="create"
        initialValues={baseValues()}
        sites={makeSites()}
        onSave={vi.fn()}
        onDismiss={vi.fn()}
      />,
    );
    const save = screen.getByTestId("building-settings-modal-save");
    expect(save).toBeDisabled();
    // Name alone is not enough — the Buildings-tab CTA opens with no
    // pre-filled site and Save must stay disabled until one is picked.
    fireEvent.change(screen.getByTestId("building-settings-name-input"), { target: { value: "Main" } });
    expect(save).toBeDisabled();
    // Pick a site from the dropdown.
    fireEvent.click(screen.getByTestId("building-settings-site-select"));
    fireEvent.click(screen.getByText("North DC"));
    expect(save).not.toBeDisabled();
  });

  it("surfaces a stale-site error and disables Save when the chosen site disappears from the list", () => {
    const { rerender } = render(
      <BuildingSettingsModal
        open
        mode="create"
        initialValues={baseValues()}
        sites={makeSites()}
        onSave={vi.fn()}
        onDismiss={vi.fn()}
      />,
    );
    fireEvent.change(screen.getByTestId("building-settings-name-input"), { target: { value: "Main" } });
    fireEvent.click(screen.getByTestId("building-settings-site-select"));
    fireEvent.click(screen.getByText("South DC"));
    expect(screen.getByTestId("building-settings-modal-save")).not.toBeDisabled();

    // Sites list refreshes and drops the chosen site (e.g. another operator
    // deleted it). The dropdown's local state still holds the stale id, but
    // Save must lock and an inline error must surface.
    rerender(
      <BuildingSettingsModal
        open
        mode="create"
        initialValues={baseValues()}
        sites={[create(SiteWithCountsSchema, { site: create(SiteSchema, { id: 7n, name: "North DC" }) })]}
        onSave={vi.fn()}
        onDismiss={vi.fn()}
      />,
    );
    expect(screen.getByTestId("building-settings-modal-save")).toBeDisabled();
    expect(screen.getByText(/Selected site is no longer available/)).toBeInTheDocument();
  });

  it("locks the Site dropdown when initialSiteId is supplied (entry from /sites/:id)", () => {
    render(
      <BuildingSettingsModal
        open
        mode="create"
        initialValues={baseValues()}
        sites={makeSites()}
        initialSiteId={7n}
        onSave={vi.fn()}
        onDismiss={vi.fn()}
      />,
    );
    const select = screen.getByTestId("building-settings-site-select");
    expect(select).toBeDisabled();
    // Site already chosen → Save unlocks as soon as a name is entered.
    fireEvent.change(screen.getByTestId("building-settings-name-input"), { target: { value: "Main" } });
    expect(screen.getByTestId("building-settings-modal-save")).not.toBeDisabled();
  });

  it("rejects negative power input with an inline error", () => {
    const onSave = vi.fn();
    render(
      <BuildingSettingsModal
        open
        mode="create"
        initialValues={baseValues()}
        sites={makeSites()}
        initialSiteId={7n}
        onSave={onSave}
        onDismiss={vi.fn()}
      />,
    );
    fireEvent.change(screen.getByTestId("building-settings-name-input"), { target: { value: "Main" } });
    fireEvent.change(screen.getByTestId("building-settings-power-input"), { target: { value: "-5" } });
    fireEvent.click(screen.getByTestId("building-settings-modal-save"));
    expect(onSave).not.toHaveBeenCalled();
  });

  it("rejects non-integer aisles", () => {
    const onSave = vi.fn();
    render(
      <BuildingSettingsModal
        open
        mode="create"
        initialValues={baseValues()}
        sites={makeSites()}
        initialSiteId={7n}
        onSave={onSave}
        onDismiss={vi.fn()}
      />,
    );
    fireEvent.change(screen.getByTestId("building-settings-name-input"), { target: { value: "Main" } });
    fireEvent.change(screen.getByTestId("building-settings-aisles-input"), { target: { value: "3.5" } });
    fireEvent.click(screen.getByTestId("building-settings-modal-save"));
    expect(onSave).not.toHaveBeenCalled();
  });

  it("rejects layout dimensions over 100", () => {
    const onSave = vi.fn();
    render(
      <BuildingSettingsModal
        open
        mode="create"
        initialValues={baseValues()}
        sites={makeSites()}
        initialSiteId={7n}
        onSave={onSave}
        onDismiss={vi.fn()}
      />,
    );
    fireEvent.change(screen.getByTestId("building-settings-name-input"), { target: { value: "Main" } });
    fireEvent.change(screen.getByTestId("building-settings-aisles-input"), { target: { value: "101" } });
    fireEvent.change(screen.getByTestId("building-settings-racks-per-aisle-input"), { target: { value: "50" } });
    fireEvent.click(screen.getByTestId("building-settings-modal-save"));
    expect(onSave).not.toHaveBeenCalled();
  });

  it("calls onSave with the parsed form values and chosen siteId on a valid submit", () => {
    const onSave = vi.fn();
    render(
      <BuildingSettingsModal
        open
        mode="create"
        initialValues={baseValues()}
        sites={makeSites()}
        onSave={onSave}
        onDismiss={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByTestId("building-settings-site-select"));
    fireEvent.click(screen.getByText("South DC"));
    fireEvent.change(screen.getByTestId("building-settings-name-input"), { target: { value: "Main" } });
    fireEvent.change(screen.getByTestId("building-settings-aisles-input"), { target: { value: "5" } });
    fireEvent.change(screen.getByTestId("building-settings-racks-per-aisle-input"), { target: { value: "8" } });
    fireEvent.click(screen.getByTestId("building-settings-modal-save"));
    expect(onSave).toHaveBeenCalledTimes(1);
    expect(onSave).toHaveBeenCalledWith(
      expect.objectContaining({
        name: "Main",
        aisles: 5,
        racksPerAisle: 8,
      }),
      9n,
    );
  });
});

describe("BuildingSettingsModal — edit mode", () => {
  it("preserves description + rack-default fields on save (pass-through pattern)", () => {
    const onSave = vi.fn();
    const initial: BuildingFormValues = {
      ...emptyBuildingFormValues(),
      name: "Existing",
      description: "preserved-desc",
      powerCapacityMw: 5,
      overheadKw: 12,
      aisles: 3,
      racksPerAisle: 4,
      physicalRackCount: 99,
      defaultRackRows: 42,
      defaultRackColumns: 21,
    };
    render(
      <BuildingSettingsModal
        open
        mode="edit"
        initialValues={initial}
        onSave={onSave}
        onDismiss={vi.fn()}
        onDeleteRequested={vi.fn()}
      />,
    );
    fireEvent.change(screen.getByTestId("building-settings-name-input"), { target: { value: "Renamed" } });
    fireEvent.click(screen.getByTestId("building-settings-modal-save"));
    expect(onSave).toHaveBeenCalledWith(
      expect.objectContaining({
        name: "Renamed",
        description: "preserved-desc",
        physicalRackCount: 99,
        defaultRackRows: 42,
        defaultRackColumns: 21,
      }),
    );
  });

  it("Delete button fires onDeleteRequested", () => {
    const onDeleteRequested = vi.fn();
    render(
      <BuildingSettingsModal
        open
        mode="edit"
        initialValues={{ ...emptyBuildingFormValues(), name: "X" }}
        onSave={vi.fn()}
        onDismiss={vi.fn()}
        onDeleteRequested={onDeleteRequested}
      />,
    );
    fireEvent.click(screen.getByTestId("building-settings-modal-delete"));
    expect(onDeleteRequested).toHaveBeenCalled();
  });
});
