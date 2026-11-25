import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import ContainerSelectionModal from "./ContainerSelectionModal";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";

const containers = [
  create(ContainerSchema, { deviceIdentifier: "ct-1", name: "North CT1", site: { id: 1n, label: "North" } }),
  create(ContainerSchema, { deviceIdentifier: "ct-2", name: "Spare CT1" }),
  create(ContainerSchema, { deviceIdentifier: "ct-3" }),
];
const props = { containers, loading: false, hasLoadError: false, onDismiss: vi.fn() };
const done = () => fireEvent.click(screen.getByRole("button", { name: "Done" }));

describe("ContainerSelectionModal", () => {
  it("lists each container with its site and returns only the selected containers", () => {
    const onSave = vi.fn();
    render(<ContainerSelectionModal {...props} selectedIdentifiers={["rig-1", "ct-1"]} onSave={onSave} />);
    expect(screen.getByText("North")).toBeInTheDocument();
    expect(screen.getAllByText("Unassigned")).toHaveLength(2);
    expect(screen.getByText("1 container selected")).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText(/ct-3/));
    fireEvent.click(screen.getByLabelText(/North CT1/));
    expect(screen.getByText("1 container selected")).toBeInTheDocument();
    done();
    expect(onSave).toHaveBeenCalledExactlyOnceWith(["ct-3"]);
  });

  it("selects and clears every container", () => {
    const onSave = vi.fn();
    render(<ContainerSelectionModal {...props} selectedIdentifiers={[]} onSave={onSave} />);
    fireEvent.click(screen.getByLabelText("All containers"));
    expect(screen.getByText("3 containers selected")).toBeInTheDocument();
    done();
    expect(onSave).toHaveBeenLastCalledWith(["ct-1", "ct-2", "ct-3"]);
    fireEvent.click(screen.getByRole("button", { name: "Select none" }));
    done();
    expect(onSave).toHaveBeenLastCalledWith([]);
  });

  it("checks the saved containers once a catalog that was loading arrives", () => {
    const onSave = vi.fn();
    const { rerender } = render(
      <ContainerSelectionModal {...props} containers={[]} loading selectedIdentifiers={["ct-2"]} onSave={onSave} />,
    );
    expect(screen.getByRole("button", { name: "Done" })).toBeDisabled();
    rerender(<ContainerSelectionModal {...props} selectedIdentifiers={["ct-2"]} onSave={onSave} />);
    expect(screen.getByLabelText(/Spare CT1/)).toBeChecked();
    done();
    expect(onSave).toHaveBeenCalledExactlyOnceWith(["ct-2"]);
  });

  it.each([
    { state: { hasLoadError: true }, title: "Couldn't load containers", canSave: false },
    { state: { containers: [] }, title: "No containers available", canSave: true },
  ])("shows $title instead of the list", ({ state, title, canSave }) => {
    render(<ContainerSelectionModal {...props} {...state} selectedIdentifiers={["ct-1"]} onSave={vi.fn()} />);
    expect(screen.getByText(title)).toBeInTheDocument();
    expect(screen.queryByLabelText("All containers")).not.toBeInTheDocument();
    if (canSave) expect(screen.getByRole("button", { name: "Done" })).toBeEnabled();
    else expect(screen.getByRole("button", { name: "Done" })).toBeDisabled();
  });
});
