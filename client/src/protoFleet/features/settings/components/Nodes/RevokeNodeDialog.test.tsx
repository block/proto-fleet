import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import RevokeNodeDialog from "./RevokeNodeDialog";

describe("RevokeNodeDialog", () => {
  it("requires a successful impact preview before revocation", () => {
    const onConfirm = vi.fn();
    const props = {
      open: true,
      nodeName: "node-01",
      affectedDeviceTypes: [] as string[],
      isLoadingImpact: true,
      impactError: "",
      onConfirm,
      onDismiss: vi.fn(),
      isSubmitting: false,
    };
    const { rerender } = render(<RevokeNodeDialog {...props} />);
    const confirm = screen.getByRole("button", { name: "Revoke node" });

    expect(confirm).toBeDisabled();
    rerender(<RevokeNodeDialog {...props} isLoadingImpact={false} impactError="Could not load affected miners." />);
    expect(confirm).toBeDisabled();
    expect(screen.getByRole("alert")).toHaveTextContent("Could not load affected miners.");

    rerender(<RevokeNodeDialog {...props} isLoadingImpact={false} affectedDeviceTypes={["proto"]} />);
    expect(screen.getByText("1 paired Proto Rig will lose connection.")).toBeInTheDocument();
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("lists every miner type separately without exposing miner identifiers", () => {
    const props = {
      open: true,
      nodeName: "node-01",
      affectedDeviceTypes: ["proto", "proto", "antminer", "antminer", "whatsminer", "new-driver", ""],
      isLoadingImpact: false,
      impactError: "",
      onConfirm: vi.fn(),
      onDismiss: vi.fn(),
      isSubmitting: false,
    };
    const { rerender } = render(<RevokeNodeDialog {...props} />);

    expect(screen.getByText("2 paired Proto Rigs will lose connection.")).toBeInTheDocument();
    expect(screen.getByText("2 paired Antminers will lose connection.")).toBeInTheDocument();
    expect(screen.getByText("1 paired WhatsMiner will lose connection.")).toBeInTheDocument();
    expect(screen.getByText("1 paired new-driver miner will lose connection.")).toBeInTheDocument();
    expect(screen.getByText("1 paired miner of unknown type will lose connection.")).toBeInTheDocument();
    expect(screen.getAllByRole("listitem")).toHaveLength(5);

    rerender(<RevokeNodeDialog {...props} affectedDeviceTypes={[]} />);
    expect(screen.getByText("No paired miners will lose connection.")).toBeInTheDocument();
  });
});
