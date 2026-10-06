import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import SiteSettingsModal from "./SiteSettingsModal";
import { emptySiteFormValues, type SiteFormValues } from "@/protoFleet/api/sites";

const baseValues = (overrides: Partial<SiteFormValues> = {}): SiteFormValues => ({
  ...emptySiteFormValues(),
  ...overrides,
});

describe("SiteSettingsModal — create mode", () => {
  it("stays clickable with an empty name and explains the problem instead of creating", async () => {
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
    // Deliberately not disabled: a dead button can't say what's missing.
    expect(continueBtn).not.toBeDisabled();

    fireEvent.click(continueBtn);

    expect(onContinue).not.toHaveBeenCalled();
    expect(screen.getByText("Enter a site name")).toBeInTheDocument();

    // Typing clears the message rather than leaving it stale under a now-valid
    // field. waitFor because Input keeps the text mounted ~200ms so the
    // collapse can animate (Input.tsx:114) — it isn't gone synchronously.
    fireEvent.change(screen.getByTestId("site-settings-name-input"), { target: { value: "North DC" } });
    await waitFor(() => expect(screen.queryByText("Enter a site name")).not.toBeInTheDocument());

    fireEvent.click(continueBtn);
    expect(onContinue).toHaveBeenCalledTimes(1);
  });

  it("reports every invalid field in one pass", () => {
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

    fireEvent.change(screen.getByTestId("site-settings-capacity-input"), { target: { value: "abc" } });
    fireEvent.click(screen.getByTestId("site-settings-modal-continue"));

    // Both, not just the first — otherwise fixing one reveals the next and the
    // operator pays a click per problem.
    expect(screen.getByText("Enter a site name")).toBeInTheDocument();
    expect(screen.getByText("Enter a number ≥ 0")).toBeInTheDocument();
    expect(onContinue).not.toHaveBeenCalled();
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
    fireEvent.change(screen.getByTestId("site-settings-address-input"), { target: { value: "123 Main St" } });
    fireEvent.change(screen.getByTestId("site-settings-city-input"), { target: { value: "Chicago" } });
    fireEvent.change(screen.getByTestId("site-settings-postal-code-input"), { target: { value: "60601" } });
    fireEvent.change(screen.getByTestId("site-settings-capacity-input"), { target: { value: "12.5" } });
    fireEvent.change(screen.getByTestId("site-settings-notes-input"), { target: { value: "rack row A1" } });

    fireEvent.click(screen.getByTestId("site-settings-state-select"));
    fireEvent.click(screen.getByText("Illinois"));

    fireEvent.click(screen.getByTestId("site-settings-modal-continue"));

    expect(onContinue).toHaveBeenCalledWith({
      name: "North DC",
      address: "123 Main St",
      locationCity: "Chicago",
      locationState: "IL",
      postalCode: "60601",
      country: "US",
      // Picking Illinois auto-seeds the timezone from inferTimezone.
      timezone: "America/Chicago",
      powerCapacityMw: 12.5,
      networkConfig: "",
      notes: "rack row A1",
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

  it("Save with nothing changed closes without calling UpdateSite", () => {
    const onSave = vi.fn();
    const onDismiss = vi.fn();
    render(
      <SiteSettingsModal
        open
        mode="edit"
        initialValues={baseValues({ name: "East DC", powerCapacityMw: 8 })}
        onSave={onSave}
        onDeleteRequested={() => undefined}
        onDismiss={onDismiss}
      />,
    );

    const save = screen.getByTestId("site-settings-modal-save");
    expect(save).not.toBeDisabled();

    fireEvent.click(save);

    // Keeping what's already there is a legitimate outcome, so this closes
    // rather than erroring — but it must not claim to have written anything.
    expect(onSave).not.toHaveBeenCalled();
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it("treats whitespace-only and capacity re-formatting as no change", () => {
    const onSave = vi.fn();
    const onDismiss = vi.fn();
    render(
      <SiteSettingsModal
        open
        mode="edit"
        initialValues={baseValues({ name: "East DC", powerCapacityMw: 8 })}
        onSave={onSave}
        onDeleteRequested={() => undefined}
        onDismiss={onDismiss}
      />,
    );

    // The no-diff check compares the same normalized shape buildValues
    // produces, so neither of these is a real edit.
    fireEvent.change(screen.getByTestId("site-settings-name-input"), { target: { value: "East DC  " } });
    fireEvent.change(screen.getByTestId("site-settings-capacity-input"), { target: { value: "8.0" } });
    fireEvent.click(screen.getByTestId("site-settings-modal-save"));

    expect(onSave).not.toHaveBeenCalled();
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it("a real edit reverted back to the original is no change again", () => {
    const onSave = vi.fn();
    const onDismiss = vi.fn();
    render(
      <SiteSettingsModal
        open
        mode="edit"
        initialValues={baseValues({ name: "East DC", powerCapacityMw: 8 })}
        onSave={onSave}
        onDeleteRequested={() => undefined}
        onDismiss={onDismiss}
      />,
    );

    const nameInput = screen.getByTestId("site-settings-name-input");
    fireEvent.change(nameInput, { target: { value: "East DC 2" } });
    fireEvent.change(nameInput, { target: { value: "East DC" } });
    fireEvent.click(screen.getByTestId("site-settings-modal-save"));

    // Not latched dirty by having been touched.
    expect(onSave).not.toHaveBeenCalled();
    expect(onDismiss).toHaveBeenCalledTimes(1);
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
