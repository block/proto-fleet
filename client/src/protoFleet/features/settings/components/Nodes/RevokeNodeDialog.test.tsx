import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import RevokeNodeDialog from "./RevokeNodeDialog";

describe("RevokeNodeDialog", () => {
  it("requires a successful impact preview before revocation", () => {
    const onConfirm = vi.fn();
    const props = {
      open: true,
      nodeName: "node-01",
      affectedMiners: [] as string[],
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

    rerender(<RevokeNodeDialog {...props} isLoadingImpact={false} affectedMiners={["miner-1"]} />);
    expect(screen.getByText("1 paired miner will lose this Node.")).toBeInTheDocument();
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });
});
