import { fireEvent, render, screen } from "@testing-library/react";
import { expect, test, vi } from "vitest";
import InlineSelector from "./InlineSelector";
import { PopoverProvider } from "@/shared/components/Popover";

const items = [
  { id: "board-1", name: "Board 1" },
  { id: "board-2", name: "Board 2" },
];

test("disables an empty selector and enables selection when options arrive", () => {
  const onSelect = vi.fn();
  const { rerender } = render(
    <PopoverProvider>
      <InlineSelector items={[]} onSelect={onSelect} />
    </PopoverProvider>,
  );
  const trigger = screen.getByRole("button");
  expect(trigger).toBeDisabled();
  fireEvent.click(trigger);
  expect(document.querySelector("[data-inline-selector-menu]")).not.toBeInTheDocument();

  rerender(
    <PopoverProvider>
      <InlineSelector items={items} currentId="board-1" onSelect={onSelect} />
    </PopoverProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Board 1" }));
  fireEvent.click(screen.getByText("Board 2"));
  expect(onSelect).toHaveBeenCalledWith("board-2");
  expect(document.querySelector("[data-inline-selector-menu]")).not.toBeInTheDocument();
});

test("closes when all options disappear and stays closed when they return", () => {
  const renderSelector = (availableItems: typeof items) => (
    <PopoverProvider>
      <InlineSelector items={availableItems} currentId="board-1" onSelect={vi.fn()} />
    </PopoverProvider>
  );
  const { rerender } = render(renderSelector(items));
  fireEvent.click(screen.getByRole("button", { name: "Board 1" }));
  expect(document.querySelector("[data-inline-selector-menu]")).toBeInTheDocument();

  rerender(renderSelector([]));
  expect(screen.getByRole("button")).toBeDisabled();
  expect(document.querySelector("[data-inline-selector-menu]")).not.toBeInTheDocument();

  rerender(renderSelector(items));
  expect(screen.getByRole("button", { name: "Board 1" })).toBeEnabled();
  expect(document.querySelector("[data-inline-selector-menu]")).not.toBeInTheDocument();
});
