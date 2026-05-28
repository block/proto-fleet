import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import SiteSettingsModal from "./SiteSettingsModal";
import { emptySiteFormValues, type SiteFormValues } from "@/protoFleet/api/sites";

const baseValues = (overrides: Partial<SiteFormValues> = {}): SiteFormValues => ({
  ...emptySiteFormValues(),
  ...overrides,
});

describe("SiteSettingsModal — create mode", () => {
  it("disables Continue until name is set", () => {
    const onContinue = vi.fn();
    render(
      <SiteSettingsModal
        open
        mode="create"
        initialValues={baseValues()}
        onContinue={onContinue}
        onDismiss={() => undefined}
      />,
    );

    const continueBtn = screen.getByTestId("site-settings-modal-continue");
    expect(continueBtn).toBeDisabled();

    fireEvent.change(screen.getByTestId("site-settings-name-input"), { target: { value: "North DC" } });
    expect(continueBtn).not.toBeDisabled();
  });

  it("invokes onContinue with typed text + selected dropdown values", () => {
    const onContinue = vi.fn();
    render(
      <SiteSettingsModal
        open
        mode="create"
        initialValues={baseValues()}
        onContinue={onContinue}
        onDismiss={() => undefined}
      />,
    );

    fireEvent.change(screen.getByTestId("site-settings-name-input"), { target: { value: "North DC" } });
    fireEvent.change(screen.getByTestId("site-settings-city-input"), { target: { value: "Chicago" } });
    fireEvent.change(screen.getByTestId("site-settings-capacity-input"), { target: { value: "12.5" } });

    // State dropdown: open + pick Illinois.
    fireEvent.click(screen.getByTestId("site-settings-state-select"));
    fireEvent.click(screen.getByText("Illinois"));

    // Timezone dropdown: open + pick Central (CT).
    fireEvent.click(screen.getByTestId("site-settings-timezone-select"));
    fireEvent.click(screen.getByText("Central (CT)"));

    fireEvent.click(screen.getByTestId("site-settings-modal-continue"));

    expect(onContinue).toHaveBeenCalledWith({
      name: "North DC",
      locationCity: "Chicago",
      locationState: "IL",
      timezone: "America/Chicago",
      powerCapacityMw: 12.5,
      networkConfig: "",
    });
  });

  it("rejects non-numeric capacity and surfaces an inline error", () => {
    const onContinue = vi.fn();
    render(
      <SiteSettingsModal
        open
        mode="create"
        initialValues={baseValues()}
        onContinue={onContinue}
        onDismiss={() => undefined}
      />,
    );

    fireEvent.change(screen.getByTestId("site-settings-name-input"), { target: { value: "North DC" } });
    fireEvent.change(screen.getByTestId("site-settings-capacity-input"), { target: { value: "abc" } });
    fireEvent.click(screen.getByTestId("site-settings-modal-continue"));

    expect(onContinue).not.toHaveBeenCalled();
    expect(screen.getByText("Enter a number ≥ 0")).toBeInTheDocument();
  });
});

describe("SiteSettingsModal — edit mode", () => {
  it("pre-populates inputs from initialValues", () => {
    render(
      <SiteSettingsModal
        open
        mode="edit"
        initialValues={baseValues({ name: "East DC", locationCity: "Boston", powerCapacityMw: 8 })}
        onSave={() => undefined}
        onDeleteRequested={() => undefined}
        onDismiss={() => undefined}
      />,
    );

    expect((screen.getByTestId("site-settings-name-input") as HTMLInputElement).value).toBe("East DC");
    expect((screen.getByTestId("site-settings-city-input") as HTMLInputElement).value).toBe("Boston");
    expect((screen.getByTestId("site-settings-capacity-input") as HTMLInputElement).value).toBe("8");
  });

  it("Save calls onSave with the typed values", () => {
    const onSave = vi.fn();
    render(
      <SiteSettingsModal
        open
        mode="edit"
        initialValues={baseValues({ name: "East DC" })}
        onSave={onSave}
        onDeleteRequested={() => undefined}
        onDismiss={() => undefined}
      />,
    );

    fireEvent.change(screen.getByTestId("site-settings-name-input"), { target: { value: "East DC 2" } });
    fireEvent.click(screen.getByTestId("site-settings-modal-save"));

    expect(onSave).toHaveBeenCalledWith(expect.objectContaining({ name: "East DC 2", powerCapacityMw: 0 }));
  });

  it("Delete triggers onDeleteRequested", () => {
    const onDeleteRequested = vi.fn();
    render(
      <SiteSettingsModal
        open
        mode="edit"
        initialValues={baseValues({ name: "East DC" })}
        onSave={() => undefined}
        onDeleteRequested={onDeleteRequested}
        onDismiss={() => undefined}
      />,
    );

    fireEvent.click(screen.getByTestId("site-settings-modal-delete"));
    expect(onDeleteRequested).toHaveBeenCalled();
  });
});

describe("SiteSettingsModal — createReturn mode", () => {
  it("renders Delete + Save instead of Cancel + Continue", () => {
    render(
      <SiteSettingsModal
        open
        mode="createReturn"
        initialValues={baseValues({ name: "North DC" })}
        onContinue={() => undefined}
        onDeleteRequested={() => undefined}
        onDismiss={() => undefined}
      />,
    );

    expect(screen.queryByTestId("site-settings-modal-continue")).toBeNull();
    expect(screen.queryByTestId("site-settings-modal-cancel")).toBeNull();
    expect(screen.getByTestId("site-settings-modal-delete")).toBeInTheDocument();
    expect(screen.getByTestId("site-settings-modal-save")).toBeInTheDocument();
  });

  it("Save invokes onContinue with the current values", () => {
    const onContinue = vi.fn();
    render(
      <SiteSettingsModal
        open
        mode="createReturn"
        initialValues={baseValues({ name: "North DC" })}
        onContinue={onContinue}
        onDeleteRequested={() => undefined}
        onDismiss={() => undefined}
      />,
    );

    fireEvent.change(screen.getByTestId("site-settings-name-input"), { target: { value: "North DC 2" } });
    fireEvent.click(screen.getByTestId("site-settings-modal-save"));

    expect(onContinue).toHaveBeenCalledWith(expect.objectContaining({ name: "North DC 2" }));
  });

  it("Delete invokes onDeleteRequested (discard pending create)", () => {
    const onDeleteRequested = vi.fn();
    render(
      <SiteSettingsModal
        open
        mode="createReturn"
        initialValues={baseValues({ name: "North DC" })}
        onContinue={() => undefined}
        onDeleteRequested={onDeleteRequested}
        onDismiss={() => undefined}
      />,
    );

    fireEvent.click(screen.getByTestId("site-settings-modal-delete"));
    expect(onDeleteRequested).toHaveBeenCalled();
  });
});
