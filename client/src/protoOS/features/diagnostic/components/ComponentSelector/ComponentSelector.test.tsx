import { fireEvent, render, screen } from "@testing-library/react";
import { expect, test, vi } from "vitest";
import ComponentSelector from "./ComponentSelector";
import { useModuleModel } from "@/protoOS/store";

vi.mock("@/protoOS/store", () => ({ useModuleModel: vi.fn() }));

test("CT1 modules only offer filters for available sections", () => {
  vi.mocked(useModuleModel).mockReturnValue("ct1");
  const onSelect = vi.fn();
  render(<ComponentSelector selectedComponent="all" onSelect={onSelect} />);

  expect(screen.queryByRole("button", { name: "Control Board" })).not.toBeInTheDocument();
  expect(screen.getAllByRole("button").map((button) => button.textContent)).toEqual([
    "All",
    "Fans",
    "Hashboards",
    "PSUs",
  ]);
  fireEvent.mouseDown(screen.getByRole("button", { name: "PSUs" }));
  expect(onSelect).toHaveBeenCalledWith("psus");
});

test("rig modules retain the Control Board filter", () => {
  vi.mocked(useModuleModel).mockReturnValue("r2");
  const onSelect = vi.fn();
  render(<ComponentSelector selectedComponent="all" onSelect={onSelect} />);

  fireEvent.mouseDown(screen.getByRole("button", { name: "Control Board" }));
  expect(onSelect).toHaveBeenCalledWith("controlBoard");
});
