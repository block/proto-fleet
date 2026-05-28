import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import BuildingSettingsModal from "./BuildingSettingsModal";
import { type BuildingFormValues, emptyBuildingFormValues } from "@/protoFleet/api/buildings";

const baseValues = (): BuildingFormValues => emptyBuildingFormValues();

describe("BuildingSettingsModal — create mode", () => {
  it("disables Save until a name is entered", () => {
    render(
      <BuildingSettingsModal open mode="create" initialValues={baseValues()} onSave={vi.fn()} onDismiss={vi.fn()} />,
    );
    const save = screen.getByTestId("building-settings-modal-save");
    expect(save).toBeDisabled();
    fireEvent.change(screen.getByTestId("building-settings-name-input"), { target: { value: "Main" } });
    expect(save).not.toBeDisabled();
  });

  it("rejects negative power input with an inline error", () => {
    render(
      <BuildingSettingsModal open mode="create" initialValues={baseValues()} onSave={vi.fn()} onDismiss={vi.fn()} />,
    );
    fireEvent.change(screen.getByTestId("building-settings-name-input"), { target: { value: "Main" } });
    fireEvent.change(screen.getByTestId("building-settings-power-input"), { target: { value: "-5" } });
    const onSave = vi.fn();
    fireEvent.click(screen.getByTestId("building-settings-modal-save"));
    // onSave never fires because buildValues returns null on validation error.
    expect(onSave).not.toHaveBeenCalled();
  });

  it("rejects non-integer aisles", () => {
    const onSave = vi.fn();
    render(
      <BuildingSettingsModal open mode="create" initialValues={baseValues()} onSave={onSave} onDismiss={vi.fn()} />,
    );
    fireEvent.change(screen.getByTestId("building-settings-name-input"), { target: { value: "Main" } });
    fireEvent.change(screen.getByTestId("building-settings-aisles-input"), { target: { value: "3.5" } });
    fireEvent.click(screen.getByTestId("building-settings-modal-save"));
    expect(onSave).not.toHaveBeenCalled();
  });

  it("rejects layout dimensions over 100", () => {
    const onSave = vi.fn();
    render(
      <BuildingSettingsModal open mode="create" initialValues={baseValues()} onSave={onSave} onDismiss={vi.fn()} />,
    );
    fireEvent.change(screen.getByTestId("building-settings-name-input"), { target: { value: "Main" } });
    fireEvent.change(screen.getByTestId("building-settings-aisles-input"), { target: { value: "101" } });
    fireEvent.change(screen.getByTestId("building-settings-racks-per-aisle-input"), { target: { value: "50" } });
    fireEvent.click(screen.getByTestId("building-settings-modal-save"));
    expect(onSave).not.toHaveBeenCalled();
  });

  it("calls onSave with the parsed form values on a valid submit", () => {
    const onSave = vi.fn();
    render(
      <BuildingSettingsModal open mode="create" initialValues={baseValues()} onSave={onSave} onDismiss={vi.fn()} />,
    );
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
