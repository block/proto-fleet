import { forwardRef, useImperativeHandle } from "react";
import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
const minerListProps = vi.hoisted(() => vi.fn());
vi.mock("@/protoFleet/components/MinerSelectionList", () => ({
  default: forwardRef((props, ref) => {
    minerListProps(props);
    useImperativeHandle(ref, () => ({ getSelection: () => ({ selectedItems: ["miner-7"] }) }));
    return <div>Miner list</div>;
  }),
}));
const { default: MinerTicketPicker } = await import("./MinerTicketPicker");
it("returns the single selected miner identifier", async () => {
  const user = userEvent.setup();
  const select = vi.fn();
  render(<MinerTicketPicker manageableSiteIds={["2", "3"]} onDismiss={vi.fn()} onSelect={select} />);
  expect(screen.getByText("Miner list")).toBeInTheDocument();
  expect(minerListProps).toHaveBeenLastCalledWith(
    expect.objectContaining({
      initialFilter: expect.objectContaining({ siteIds: [2n, 3n], includeUnassigned: true }),
      filterConfig: expect.objectContaining({ showSiteFilter: false }),
    }),
  );
  await user.click(screen.getByRole("button", { name: "Use selected miner" }));
  expect(select).toHaveBeenCalledWith("miner-7");
});
