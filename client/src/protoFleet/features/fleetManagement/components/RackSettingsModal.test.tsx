import type { ComponentProps } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import RackSettingsModal from "./RackSettingsModal";
import {
  PerRackCreateErrorReason,
  RackCoolingType,
  RackOrderIndex,
} from "@/protoFleet/api/generated/device_set/v1/device_set_pb";

type RackSettingsModalProps = ComponentProps<typeof RackSettingsModal>;
// Typing the stub off the prop itself keeps the resolved value honest: a mock
// that returned the wrong error shape would fail here rather than at runtime.
type SubmitBulk = NonNullable<RackSettingsModalProps["onSubmitBulk"]>;

// The modal fetches zone suggestions and rack types on mount and blocks its CTA
// until both land, so both callbacks fire synchronously here.
const mockListRackZones = vi.fn(({ onSuccess, onFinally }) => {
  onSuccess?.([]);
  onFinally?.();
});
const mockListRackTypes = vi.fn(({ onSuccess, onFinally }) => {
  onSuccess?.([]);
  onFinally?.();
});

vi.mock("@/protoFleet/api/useDeviceSets", async (importActual) => ({
  ...(await importActual<typeof import("@/protoFleet/api/useDeviceSets")>()),
  useDeviceSets: () => ({
    listRackZones: mockListRackZones,
    listRackTypes: mockListRackTypes,
  }),
}));

vi.mock("@/protoFleet/api/SitesContext", () => ({
  useSitesContext: () => ({ sites: [] }),
}));

vi.mock("@/protoFleet/api/buildings", () => ({
  useBuildings: () => ({ listBuildingsBySite: vi.fn() }),
}));

vi.mock("@/protoFleet/store", () => ({
  useHasPermission: () => true,
}));

// Geometry is the one thing the bulk form still requires per submit, and no rack
// types come back from the stub, so every test fills it.
const fillGeometry = () => {
  fireEvent.change(screen.getByLabelText("Columns"), { target: { value: "3" } });
  fireEvent.change(screen.getByLabelText("Rows"), { target: { value: "4" } });
};

const renderModal = (props: Partial<RackSettingsModalProps> = {}) =>
  render(<RackSettingsModal show existingRacks={[]} onDismiss={vi.fn()} {...props} />);

describe("RackSettingsModal — bulk create", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("hides the Single / Multiple toggle when no batch handler is wired", () => {
    renderModal({ onSubmit: vi.fn() });
    expect(screen.queryByText("Multiple")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Label")).toBeInTheDocument();
  });

  it("opens on the Multiple form when the host preselected it", () => {
    renderModal({ onSubmit: vi.fn(), onSubmitBulk: vi.fn(), initialCreateVariant: "multiple" });
    // The single-rack Label field is replaced by the batch's generators.
    expect(screen.queryByLabelText("Label")).not.toBeInTheDocument();
    expect(screen.getByTestId("rack-bulk-count-input")).toBeInTheDocument();
    expect(screen.getByTestId("rack-bulk-prefix-input")).toBeInTheDocument();
  });

  it("offers no batch form when editing an existing rack", () => {
    renderModal({
      existingRack: true,
      initialFormData: {
        label: "R-1",
        zone: "",
        rows: 4,
        columns: 3,
        orderIndex: RackOrderIndex.BOTTOM_LEFT,
        coolingType: RackCoolingType.AIR,
      },
      onSubmit: vi.fn(),
      onSubmitBulk: vi.fn(),
      initialCreateVariant: "multiple",
    });
    // A rack that exists has one label to edit, not a series to generate — so
    // the toggle is absent even though the host wired the batch handler.
    expect(screen.queryByText("Multiple")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Label")).toBeInTheDocument();
  });

  it("previews exactly the labels it will submit, and submits them with the shared geometry", async () => {
    const onSubmitBulk = vi.fn<SubmitBulk>().mockResolvedValue([]);
    renderModal({ onSubmitBulk, initialCreateVariant: "multiple" });

    fireEvent.change(screen.getByTestId("rack-bulk-count-input"), { target: { value: "3" } });
    fireEvent.change(screen.getByTestId("rack-bulk-prefix-input"), { target: { value: "R-" } });
    fillGeometry();

    // Counter starts at 1; the default scale is 1 digit, so no padding yet.
    expect(screen.getByTestId("rack-bulk-preview-row-0")).toHaveTextContent("R-1");
    expect(screen.getByTestId("rack-bulk-preview-row-2")).toHaveTextContent("R-3");
    expect(screen.getByText("3 racks to create")).toBeInTheDocument();

    fireEvent.click(screen.getByText("Create racks"));
    await waitFor(() => expect(onSubmitBulk).toHaveBeenCalledTimes(1));
    // The preview is the payload: same strings, same order.
    expect(onSubmitBulk).toHaveBeenCalledWith(
      ["R-1", "R-2", "R-3"].map((label) => ({
        label,
        rows: 4,
        columns: 3,
        zone: "",
        orderIndex: RackOrderIndex.BOTTOM_LEFT,
        coolingType: RackCoolingType.AIR,
      })),
      // Nothing was picked, so the batch lands unplaced.
      { siteId: undefined, buildingId: undefined },
    );
  });

  it("names every missing bulk field on one click instead of disabling the CTA", async () => {
    const onSubmitBulk = vi.fn();
    renderModal({ onSubmitBulk, initialCreateVariant: "multiple" });

    const create = screen.getByText("Create racks");
    expect(create).not.toBeDisabled();
    fireEvent.click(create);
    expect(onSubmitBulk).not.toHaveBeenCalled();
    // All of them, not one per click.
    expect(screen.getByText("A label prefix is required")).toBeInTheDocument();
    expect(screen.getByText("Enter how many racks to create")).toBeInTheDocument();
    expect(screen.getByText("Columns must be a whole number between 1 and 12")).toBeInTheDocument();
    expect(screen.getByText("Rows must be a whole number between 1 and 12")).toBeInTheDocument();
    // The single-rack Label check must not fire — that field isn't on screen.
    expect(screen.queryByText("A label is required")).not.toBeInTheDocument();

    fireEvent.change(screen.getByTestId("rack-bulk-count-input"), { target: { value: "2" } });
    fireEvent.change(screen.getByTestId("rack-bulk-prefix-input"), { target: { value: "R-" } });
    fillGeometry();
    // waitFor because Input keeps cleared error text mounted ~200ms so the
    // collapse can animate.
    await waitFor(() => expect(screen.queryByText("A label prefix is required")).not.toBeInTheDocument());
    fireEvent.click(create);
    await waitFor(() => expect(onSubmitBulk).toHaveBeenCalledTimes(1));
  });

  it("bounds the counter scale on submit rather than constraining the input", async () => {
    const onSubmitBulk = vi.fn();
    renderModal({ onSubmitBulk, initialCreateVariant: "multiple" });

    fireEvent.change(screen.getByTestId("rack-bulk-count-input"), { target: { value: "2" } });
    fireEvent.change(screen.getByTestId("rack-bulk-prefix-input"), { target: { value: "R-" } });
    fillGeometry();
    // 9 digits of zero-padding is past the bound; the field accepts the digit
    // and the submit explains why it can't be used.
    fireEvent.change(screen.getByTestId("rack-bulk-counter-scale-input"), { target: { value: "9" } });

    fireEvent.click(screen.getByText("Create racks"));
    expect(onSubmitBulk).not.toHaveBeenCalled();
    expect(screen.getByText("Must be 1–6")).toBeInTheDocument();
  });

  it("refuses a batch whose generated labels are longer than the server accepts", async () => {
    const onSubmitBulk = vi.fn();
    renderModal({ onSubmitBulk, initialCreateVariant: "multiple" });

    fireEvent.change(screen.getByTestId("rack-bulk-count-input"), { target: { value: "2" } });
    fillGeometry();
    // A prefix that fits on its own (the field allows 100), plus 6 digits of
    // zero-padding, produces 103-character labels. The counter width is what
    // pushes it over, so the prefix field alone can't catch this.
    fireEvent.change(screen.getByTestId("rack-bulk-prefix-input"), { target: { value: "R".repeat(97) } });
    fireEvent.change(screen.getByTestId("rack-bulk-counter-scale-input"), { target: { value: "6" } });

    // Flagged per row as the operator types, before any submit.
    expect(screen.getByTestId("rack-bulk-preview-row-0")).toHaveTextContent("Over 100 characters");

    fireEvent.click(screen.getByText("Create racks"));
    // Never dispatched: CreateRacks would refuse the whole batch on
    // NewRack.label's max_len and come back as a generic error.
    expect(onSubmitBulk).not.toHaveBeenCalled();
    expect(screen.getByText("Labels must be 100 characters or fewer, counter included")).toBeInTheDocument();

    // Narrowing the counter brings every row back inside the cap.
    fireEvent.change(screen.getByTestId("rack-bulk-counter-scale-input"), { target: { value: "1" } });
    await waitFor(() =>
      expect(screen.getByTestId("rack-bulk-preview-row-0")).not.toHaveTextContent("Over 100 characters"),
    );
    fireEvent.click(screen.getByText("Create racks"));
    await waitFor(() => expect(onSubmitBulk).toHaveBeenCalledTimes(1));
  });

  it("marks the rows the server rejected and keeps the form open", async () => {
    const onSubmitBulk = vi
      .fn<SubmitBulk>()
      .mockResolvedValue([{ index: 1, label: "R-02", reason: PerRackCreateErrorReason.DUPLICATE_LABEL_IN_ORG }]);
    const onDismiss = vi.fn();
    renderModal({ onSubmitBulk, onDismiss, initialCreateVariant: "multiple" });

    fireEvent.change(screen.getByTestId("rack-bulk-count-input"), { target: { value: "2" } });
    fireEvent.change(screen.getByTestId("rack-bulk-prefix-input"), { target: { value: "R-" } });
    fillGeometry();
    fireEvent.click(screen.getByText("Create racks"));

    // Org-wide wording, not "at this building": the colliding rack may be in
    // another site entirely.
    await waitFor(() =>
      expect(screen.getByTestId("rack-bulk-preview-row-1")).toHaveTextContent("Already used by another rack"),
    );
    expect(screen.getByTestId("rack-bulk-preview-row-0")).not.toHaveTextContent("Already used");
    // The host owns closing; a rejected batch leaves the form up for a retry.
    expect(onDismiss).not.toHaveBeenCalled();

    // Editing a bulk field invalidates the marks — they point at indexes in the
    // payload that was submitted, not the one now on screen.
    fireEvent.change(screen.getByTestId("rack-bulk-prefix-input"), { target: { value: "RK-" } });
    await waitFor(() =>
      expect(screen.getByTestId("rack-bulk-preview-row-1")).not.toHaveTextContent("Already used by another rack"),
    );
  });

  it("keeps the single-rack path submitting one rack when the toggle stays on Single", async () => {
    const onSubmit = vi.fn();
    const onSubmitBulk = vi.fn();
    renderModal({ onSubmit, onSubmitBulk });

    fireEvent.change(screen.getByLabelText("Label"), { target: { value: "R-99" } });
    fillGeometry();
    fireEvent.click(screen.getByText("Create rack"));

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    expect(onSubmitBulk).not.toHaveBeenCalled();
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ label: "R-99", rows: 4, columns: 3 }));
  });
});
