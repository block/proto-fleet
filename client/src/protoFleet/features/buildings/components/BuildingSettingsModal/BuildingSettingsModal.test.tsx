import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, type Mock, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import BuildingSettingsModal from "./BuildingSettingsModal";
import {
  type BuildingFormValues,
  type BulkCreateBuildingError,
  emptyBuildingFormValues,
  type NewBuildingInput,
} from "@/protoFleet/api/buildings";
import { PerBuildingCreateErrorReason } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { SiteSchema, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";

const baseValues = (): BuildingFormValues => emptyBuildingFormValues();

const makeSites = () => [
  create(SiteWithCountsSchema, { site: create(SiteSchema, { id: 7n, name: "North DC" }) }),
  create(SiteWithCountsSchema, { site: create(SiteSchema, { id: 9n, name: "South DC" }) }),
];

describe("BuildingSettingsModal — create mode", () => {
  it("stays clickable with no site or name and names both problems", async () => {
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
    const save = screen.getByTestId("building-settings-modal-save");
    // Deliberately not disabled: a dead button can't say what's missing.
    expect(save).not.toBeDisabled();

    fireEvent.click(save);
    expect(onSave).not.toHaveBeenCalled();
    // Both, not just the first — otherwise fixing one reveals the next and the
    // operator pays a click per problem.
    expect(screen.getByText("Enter a building name")).toBeInTheDocument();
    expect(screen.getByText("Select a site")).toBeInTheDocument();

    // Name alone is not enough — the Buildings-tab CTA opens with no pre-filled
    // site, so the site error survives on its own. waitFor because Input keeps
    // the cleared text mounted ~200ms so the collapse can animate.
    fireEvent.change(screen.getByTestId("building-settings-name-input"), { target: { value: "Main" } });
    await waitFor(() => expect(screen.queryByText("Enter a building name")).not.toBeInTheDocument());
    fireEvent.click(save);
    expect(onSave).not.toHaveBeenCalled();
    expect(screen.getByText("Select a site")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("building-settings-site-select"));
    fireEvent.click(screen.getByText("North DC"));
    fireEvent.click(save);
    expect(onSave).toHaveBeenCalledWith(expect.objectContaining({ name: "Main" }), 7n);
  });

  it("surfaces a stale-site error and refuses the submit when the chosen site disappears from the list", () => {
    const onSave = vi.fn();
    const { rerender } = render(
      <BuildingSettingsModal
        open
        mode="create"
        initialValues={baseValues()}
        sites={makeSites()}
        onSave={onSave}
        onDismiss={vi.fn()}
      />,
    );
    fireEvent.change(screen.getByTestId("building-settings-name-input"), { target: { value: "Main" } });
    fireEvent.click(screen.getByTestId("building-settings-site-select"));
    fireEvent.click(screen.getByText("South DC"));
    expect(screen.queryByText(/no longer available/)).not.toBeInTheDocument();

    // Sites list refreshes and drops the chosen site (e.g. another operator
    // deleted it). The dropdown's local state still holds the stale id, so the
    // error surfaces without waiting for a click — unlike every other check
    // here, this one can go wrong while the operator is doing nothing.
    rerender(
      <BuildingSettingsModal
        open
        mode="create"
        initialValues={baseValues()}
        sites={[create(SiteWithCountsSchema, { site: create(SiteSchema, { id: 7n, name: "North DC" }) })]}
        onSave={onSave}
        onDismiss={vi.fn()}
      />,
    );
    expect(screen.getByText(/Selected site is no longer available/)).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("building-settings-modal-save"));
    expect(onSave).not.toHaveBeenCalled();
  });

  it("locks the Site dropdown when initialSiteId is supplied (entry from /sites/:id)", () => {
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
    const select = screen.getByTestId("building-settings-site-select");
    expect(select).toBeDisabled();
    // Site already resolved from the prop, so a name is all that's left to give.
    fireEvent.change(screen.getByTestId("building-settings-name-input"), { target: { value: "Main" } });
    fireEvent.click(screen.getByTestId("building-settings-modal-save"));
    expect(onSave).toHaveBeenCalledWith(expect.objectContaining({ name: "Main" }), 7n);
  });

  it("preserves the capacity fields it no longer exposes", () => {
    const onSave = vi.fn();
    render(
      <BuildingSettingsModal
        open
        mode="create"
        initialValues={{ ...baseValues(), powerCapacityMw: 5, overheadKw: 12 }}
        sites={makeSites()}
        initialSiteId={7n}
        onSave={onSave}
        onDismiss={vi.fn()}
      />,
    );
    // Power capacity and Overhead are not in the form (nothing consumes them
    // yet), so they must pass through rather than reset to 0.
    expect(screen.queryByTestId("building-settings-power-input")).not.toBeInTheDocument();
    expect(screen.queryByTestId("building-settings-overhead-input")).not.toBeInTheDocument();
    fireEvent.change(screen.getByTestId("building-settings-name-input"), { target: { value: "Main" } });
    fireEvent.click(screen.getByTestId("building-settings-modal-save"));
    expect(onSave).toHaveBeenCalledWith(expect.objectContaining({ powerCapacityMw: 5, overheadKw: 12 }), 7n);
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
    expect(screen.getByText("Whole number ≥ 0")).toBeInTheDocument();
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
    expect(screen.getByText("Must be ≤ 100")).toBeInTheDocument();
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

type BulkSaveMock = Mock<(buildings: NewBuildingInput[], siteId: bigint) => Promise<BulkCreateBuildingError[]>>;

describe("BuildingSettingsModal — create mode, Multiple", () => {
  // The toggle is rendered as a SegmentedControl, whose segments commit on
  // mousedown rather than click.
  const selectMultiple = () => fireEvent.mouseDown(screen.getByText("Multiple"));

  const renderBulk = (overrides: { onSaveBulk?: BulkSaveMock; existingBuildingNames?: string[] } = {}) => {
    const onSaveBulk: BulkSaveMock = overrides.onSaveBulk ?? vi.fn(async () => []);
    render(
      <BuildingSettingsModal
        open
        mode="create"
        initialValues={baseValues()}
        sites={makeSites()}
        initialSiteId={7n}
        onSave={vi.fn()}
        onSaveBulk={onSaveBulk}
        existingBuildingNames={overrides.existingBuildingNames}
        onDismiss={vi.fn()}
      />,
    );
    return onSaveBulk;
  };

  it("opens on the Multiple form when the host preselected it", () => {
    render(
      <BuildingSettingsModal
        open
        mode="create"
        initialValues={baseValues()}
        sites={makeSites()}
        initialSiteId={7n}
        onSave={vi.fn()}
        onSaveBulk={vi.fn(async () => [])}
        initialCreateVariant="multiple"
        onDismiss={vi.fn()}
      />,
    );
    // The picker's "Create multiple buildings" button lands here directly, so the
    // bulk fields are up without touching the toggle.
    expect(screen.getByTestId("building-settings-bulk-count-input")).toBeInTheDocument();
    expect(screen.queryByTestId("building-settings-name-input")).not.toBeInTheDocument();
    expect(screen.getByTestId("building-settings-modal-save")).toHaveTextContent("Create buildings");
  });

  it("hides the Single / Multiple toggle when the host wired no batch handler", () => {
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
    expect(screen.queryByTestId("segmented-control")).not.toBeInTheDocument();
  });

  it("takes the row count and counter start as numeric fields", () => {
    renderBulk();
    selectMultiple();
    expect(screen.getByTestId("building-settings-bulk-count-input")).toHaveAttribute("type", "number");
    expect(screen.getByTestId("building-settings-bulk-counter-start-input")).toHaveAttribute("type", "number");
  });

  it("asks for the count and prefix it needs instead of locking the CTA", async () => {
    const onSaveBulk = renderBulk();
    selectMultiple();
    const save = screen.getByTestId("building-settings-modal-save");
    expect(save).toHaveTextContent("Create buildings");
    expect(save).not.toBeDisabled();

    fireEvent.click(save);
    expect(onSaveBulk).not.toHaveBeenCalled();
    expect(screen.getByText("Enter how many buildings to create")).toBeInTheDocument();
    expect(screen.getByText("Enter a name prefix")).toBeInTheDocument();

    // A count with no prefix would create buildings named "1", "2", … — not
    // what anyone means, so the prefix problem outlives the count one.
    fireEvent.change(screen.getByTestId("building-settings-bulk-count-input"), { target: { value: "3" } });
    await waitFor(() => expect(screen.queryByText("Enter how many buildings to create")).not.toBeInTheDocument());
    fireEvent.click(save);
    expect(onSaveBulk).not.toHaveBeenCalled();
    expect(screen.getByText("Enter a name prefix")).toBeInTheDocument();

    fireEvent.change(screen.getByTestId("building-settings-bulk-prefix-input"), { target: { value: "Bldg-" } });
    fireEvent.click(save);
    expect(onSaveBulk).toHaveBeenCalledTimes(1);
  });

  it("previews the generated names as read-only rows and submits exactly those names", async () => {
    const onSaveBulk = renderBulk();
    selectMultiple();
    fireEvent.change(screen.getByTestId("building-settings-bulk-count-input"), { target: { value: "3" } });
    fireEvent.change(screen.getByTestId("building-settings-bulk-prefix-input"), { target: { value: "Bldg-" } });
    fireEvent.change(screen.getByTestId("building-settings-bulk-counter-start-input"), { target: { value: "5" } });
    fireEvent.change(screen.getByTestId("building-settings-bulk-counter-scale-input"), { target: { value: "4" } });

    const preview = screen.getByTestId("building-settings-bulk-preview");
    expect(preview).toHaveTextContent("3 buildings to create");
    expect(screen.getByTestId("building-settings-bulk-preview-row-0")).toHaveTextContent("Bldg-0005");
    expect(screen.getByTestId("building-settings-bulk-preview-row-2")).toHaveTextContent("Bldg-0007");
    // The preview mirrors the payload; it is not a form.
    expect(preview.querySelectorAll("input")).toHaveLength(0);

    fireEvent.click(screen.getByTestId("building-settings-modal-save"));
    expect(onSaveBulk).toHaveBeenCalledWith(
      [
        { name: "Bldg-0005", aisles: 0, racksPerAisle: 0 },
        { name: "Bldg-0006", aisles: 0, racksPerAisle: 0 },
        { name: "Bldg-0007", aisles: 0, racksPerAisle: 0 },
      ],
      7n,
    );
  });

  it("applies one layout to every building in the batch", () => {
    const onSaveBulk = renderBulk();
    selectMultiple();
    fireEvent.change(screen.getByTestId("building-settings-bulk-count-input"), { target: { value: "2" } });
    fireEvent.change(screen.getByTestId("building-settings-bulk-prefix-input"), { target: { value: "B-" } });
    fireEvent.change(screen.getByTestId("building-settings-aisles-input"), { target: { value: "4" } });
    fireEvent.change(screen.getByTestId("building-settings-racks-per-aisle-input"), { target: { value: "10" } });
    fireEvent.click(screen.getByTestId("building-settings-modal-save"));

    expect(onSaveBulk).toHaveBeenCalledWith(
      [
        { name: "B-1", aisles: 4, racksPerAisle: 10 },
        { name: "B-2", aisles: 4, racksPerAisle: 10 },
      ],
      7n,
    );
  });

  it("blocks the batch when the shared layout is over the dimension cap", () => {
    const onSaveBulk = renderBulk();
    selectMultiple();
    fireEvent.change(screen.getByTestId("building-settings-bulk-count-input"), { target: { value: "2" } });
    fireEvent.change(screen.getByTestId("building-settings-bulk-prefix-input"), { target: { value: "B-" } });

    // The layout is optional, but a bad value must not reach 500 rows at once.
    fireEvent.change(screen.getByTestId("building-settings-aisles-input"), { target: { value: "101" } });
    fireEvent.click(screen.getByTestId("building-settings-modal-save"));
    expect(onSaveBulk).not.toHaveBeenCalled();
    expect(screen.getByText("Must be ≤ 100")).toBeInTheDocument();
  });

  it("marks the row and refuses the batch when a generated name is already at the site", async () => {
    const onSaveBulk = renderBulk({ existingBuildingNames: ["Bldg-2"] });
    selectMultiple();
    fireEvent.change(screen.getByTestId("building-settings-bulk-count-input"), { target: { value: "3" } });
    fireEvent.change(screen.getByTestId("building-settings-bulk-prefix-input"), { target: { value: "Bldg-" } });

    expect(screen.getByTestId("building-settings-bulk-preview-row-1")).toHaveTextContent("Already used at this site");
    fireEvent.click(screen.getByTestId("building-settings-modal-save"));
    expect(onSaveBulk).not.toHaveBeenCalled();
    // The offending row is already marked, so a bare no-op click would look
    // inert — the form-level callout says why nothing happened.
    expect(screen.getByTestId("building-settings-modal-form-error")).toBeInTheDocument();

    // Shifting the counter past the collision clears it without touching the
    // count — the whole point of previewing before the round trip.
    fireEvent.change(screen.getByTestId("building-settings-bulk-counter-start-input"), { target: { value: "3" } });
    await waitFor(() => expect(screen.queryByTestId("building-settings-modal-form-error")).not.toBeInTheDocument());
    fireEvent.click(screen.getByTestId("building-settings-modal-save"));
    expect(onSaveBulk).toHaveBeenCalledWith(
      [
        { name: "Bldg-3", aisles: 0, racksPerAisle: 0 },
        { name: "Bldg-4", aisles: 0, racksPerAisle: 0 },
        { name: "Bldg-5", aisles: 0, racksPerAisle: 0 },
      ],
      7n,
    );
  });

  it("refuses a batch whose generated names are longer than the server accepts", async () => {
    const onSaveBulk = renderBulk();
    selectMultiple();
    fireEvent.change(screen.getByTestId("building-settings-bulk-count-input"), { target: { value: "2" } });
    // A prefix that fits on its own (the field allows 255), plus 6 digits of
    // zero-padding, produces 258-character names. The counter width is what
    // pushes it over, so the prefix field alone can't catch this.
    fireEvent.change(screen.getByTestId("building-settings-bulk-prefix-input"), { target: { value: "B".repeat(252) } });
    fireEvent.change(screen.getByTestId("building-settings-bulk-counter-scale-input"), { target: { value: "6" } });

    // Flagged per row as the operator types, before any submit.
    expect(screen.getByTestId("building-settings-bulk-preview-row-0")).toHaveTextContent("Over 255 characters");

    fireEvent.click(screen.getByTestId("building-settings-modal-save"));
    // Never dispatched: CreateBuildings would refuse the whole batch on
    // NewBuilding.name's max_len and come back as a generic error.
    expect(onSaveBulk).not.toHaveBeenCalled();
    expect(screen.getByText("Names must be 255 characters or fewer, counter included")).toBeInTheDocument();

    fireEvent.change(screen.getByTestId("building-settings-bulk-counter-scale-input"), { target: { value: "1" } });
    await waitFor(() =>
      expect(screen.getByTestId("building-settings-bulk-preview-row-0")).not.toHaveTextContent("Over 255 characters"),
    );
    fireEvent.click(screen.getByTestId("building-settings-modal-save"));
    expect(onSaveBulk).toHaveBeenCalledTimes(1);
  });

  it("bounds the counter scale on submit rather than constraining the input", async () => {
    const onSaveBulk = renderBulk();
    selectMultiple();
    fireEvent.change(screen.getByTestId("building-settings-bulk-count-input"), { target: { value: "2" } });
    fireEvent.change(screen.getByTestId("building-settings-bulk-prefix-input"), { target: { value: "B-" } });

    const scale = screen.getByTestId("building-settings-bulk-counter-scale-input");
    fireEvent.change(scale, { target: { value: "9" } });
    fireEvent.click(screen.getByTestId("building-settings-modal-save"));
    expect(onSaveBulk).not.toHaveBeenCalled();
    expect(screen.getByText("Must be 1–6")).toBeInTheDocument();

    // Non-digits never land in the field at all, so 0 and 7–9 are the only ways
    // to be out of range.
    fireEvent.change(scale, { target: { value: "3" } });
    await waitFor(() => expect(screen.queryByText("Must be 1–6")).not.toBeInTheDocument());
    fireEvent.click(screen.getByTestId("building-settings-modal-save"));
    expect(onSaveBulk).toHaveBeenCalledWith(
      [
        { name: "B-001", aisles: 0, racksPerAisle: 0 },
        { name: "B-002", aisles: 0, racksPerAisle: 0 },
      ],
      7n,
    );
  });

  it("marks the rows the server rejected and clears them on the next edit", async () => {
    const onSaveBulk = vi
      .fn()
      .mockResolvedValue([{ index: 1, name: "Bldg-2", reason: PerBuildingCreateErrorReason.DUPLICATE_NAME_AT_SITE }]);
    renderBulk({ onSaveBulk });
    selectMultiple();
    fireEvent.change(screen.getByTestId("building-settings-bulk-count-input"), { target: { value: "3" } });
    fireEvent.change(screen.getByTestId("building-settings-bulk-prefix-input"), { target: { value: "Bldg-" } });
    fireEvent.click(screen.getByTestId("building-settings-modal-save"));

    // The batch is all-or-nothing, so the modal stays open with the offending
    // row called out rather than reporting a partial success.
    await waitFor(() =>
      expect(screen.getByTestId("building-settings-bulk-preview-row-1")).toHaveTextContent("Already used at this site"),
    );

    // Those indexes describe the payload that was sent; editing the fields
    // changes the payload, so the marks can't be carried over.
    fireEvent.change(screen.getByTestId("building-settings-bulk-counter-start-input"), { target: { value: "9" } });
    expect(screen.getByTestId("building-settings-bulk-preview-row-1")).not.toHaveTextContent("Already used");
  });
});

describe("BuildingSettingsModal — edit mode", () => {
  it("preserves description, capacity + rack-default fields on save (pass-through pattern)", () => {
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
        powerCapacityMw: 5,
        overheadKw: 12,
        physicalRackCount: 99,
        defaultRackRows: 42,
        defaultRackColumns: 21,
      }),
    );
  });

  it("Save with nothing changed closes without calling UpdateBuilding", () => {
    const initial: BuildingFormValues = {
      ...emptyBuildingFormValues(),
      name: "Existing",
      powerCapacityMw: 5,
      aisles: 3,
      racksPerAisle: 4,
    };
    const onSave = vi.fn();
    const onDismiss = vi.fn();
    render(
      <BuildingSettingsModal
        open
        mode="edit"
        initialValues={initial}
        onSave={onSave}
        onDismiss={onDismiss}
        onDeleteRequested={vi.fn()}
      />,
    );

    const save = screen.getByTestId("building-settings-modal-save");
    expect(save).not.toBeDisabled();

    // Keeping what's already there is a legitimate outcome, so this closes
    // rather than erroring — but UpdateBuilding must not report a write it
    // didn't make.
    fireEvent.click(save);
    expect(onSave).not.toHaveBeenCalled();
    expect(onDismiss).toHaveBeenCalledTimes(1);

    // Re-typing the same value in a different format is not a change — the
    // check compares the parsed form, not the raw text.
    fireEvent.change(screen.getByTestId("building-settings-aisles-input"), { target: { value: "03" } });
    fireEvent.click(save);
    expect(onSave).not.toHaveBeenCalled();

    fireEvent.change(screen.getByTestId("building-settings-aisles-input"), { target: { value: "6" } });
    fireEvent.click(save);
    expect(onSave).toHaveBeenCalledTimes(1);

    // Back to the original → clean again, not latched dirty by having been
    // touched.
    fireEvent.change(screen.getByTestId("building-settings-aisles-input"), { target: { value: "3" } });
    fireEvent.click(save);
    expect(onSave).toHaveBeenCalledTimes(1);
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
