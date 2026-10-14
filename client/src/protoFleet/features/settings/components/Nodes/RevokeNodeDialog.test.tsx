import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import RevokeNodeDialog from "./RevokeNodeDialog";

describe("RevokeNodeDialog", () => {
  it("groups ASIC-rs miners by manufacturer and model instead of driver", () => {
    render(
      <RevokeNodeDialog
        open
        nodeName="node-01"
        affectedDevices={[
          { manufacturer: "Bitmain", model: "S19" },
          { manufacturer: "Bitmain", model: "S19" },
          { manufacturer: "Bitmain", model: "S21" },
          { manufacturer: "MicroBT", model: "M60" },
        ]}
        isLoadingImpact={false}
        impactError=""
        onConfirm={vi.fn()}
        onDismiss={vi.fn()}
        isSubmitting={false}
      />,
    );

    expect(screen.getByText("2 paired Bitmain S19 miners will lose connection.")).toBeInTheDocument();
    expect(screen.getByText("1 paired Bitmain S21 miner will lose connection.")).toBeInTheDocument();
    expect(screen.getByText("1 paired MicroBT M60 miner will lose connection.")).toBeInTheDocument();
    expect(screen.queryByText(/ASIC-RS/)).not.toBeInTheDocument();
  });

  it("requires a successful impact preview before revocation", () => {
    const onConfirm = vi.fn();
    const props = {
      open: true,
      nodeName: "node-01",
      affectedDevices: [] as { manufacturer: string; model: string }[],
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

    rerender(
      <RevokeNodeDialog
        {...props}
        isLoadingImpact={false}
        affectedDevices={[{ manufacturer: "Proto", model: "Rig" }]}
      />,
    );
    expect(screen.getByText("1 paired Proto Rig miner will lose connection.")).toBeInTheDocument();
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("keeps partial hardware details separate and handles an empty preview", () => {
    const props = {
      open: true,
      nodeName: "node-01",
      affectedDevices: [
        { manufacturer: " Bitmain ", model: " S19 " },
        { manufacturer: "Bitmain", model: "S19" },
        { manufacturer: "Other", model: "S19" },
        { manufacturer: "Bitmain", model: "" },
        { manufacturer: "", model: "S19" },
        { manufacturer: " ", model: " " },
      ],
      isLoadingImpact: false,
      impactError: "",
      onConfirm: vi.fn(),
      onDismiss: vi.fn(),
      isSubmitting: false,
    };
    const { rerender } = render(<RevokeNodeDialog {...props} />);

    expect(screen.getByText("2 paired Bitmain S19 miners will lose connection.")).toBeInTheDocument();
    expect(screen.getByText("1 paired Other S19 miner will lose connection.")).toBeInTheDocument();
    expect(screen.getByText("1 paired Bitmain Unknown model miner will lose connection.")).toBeInTheDocument();
    expect(screen.getByText("1 paired Unknown manufacturer S19 miner will lose connection.")).toBeInTheDocument();
    expect(
      screen.getByText("1 paired Unknown manufacturer Unknown model miner will lose connection."),
    ).toBeInTheDocument();
    expect(screen.getAllByRole("listitem")).toHaveLength(5);

    rerender(<RevokeNodeDialog {...props} affectedDevices={[]} />);
    expect(screen.getByText("No paired miners will lose connection.")).toBeInTheDocument();
  });
});
