import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import ListPagination from "./ListPagination";

it("shows the current result range and exposes horizontal page controls", () => {
  const previous = vi.fn();
  const next = vi.fn();
  render(
    <ListPagination
      firstItemIndex={51}
      lastItemIndex={75}
      hasPreviousPage
      total={75}
      itemName="parts"
      hasNextPage={false}
      onPrevious={previous}
      onNext={next}
    />,
  );

  expect(screen.getByText("Showing 51–75 of 75 parts")).toBeInTheDocument();
  const previousButton = screen.getByRole("button", { name: "Previous page" });
  const nextButton = screen.getByRole("button", { name: "Next page" });
  expect(previousButton).toBeEnabled();
  expect(nextButton).toBeDisabled();
  fireEvent.click(previousButton);
  expect(previous).toHaveBeenCalledTimes(1);
  expect(next).not.toHaveBeenCalled();
});

it("keeps totals unknown for a cursor page that has more results", () => {
  render(
    <ListPagination
      firstItemIndex={1}
      lastItemIndex={100}
      itemName="mining assets"
      hasPreviousPage={false}
      hasNextPage
      onPrevious={vi.fn()}
      onNext={vi.fn()}
    />,
  );
  expect(screen.getByText("Showing 1–100 mining assets")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Previous page" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Next page" })).toBeEnabled();
});

it("lets an operator return from a cursor page emptied by an inventory change", () => {
  render(
    <ListPagination
      firstItemIndex={101}
      lastItemIndex={100}
      total={100}
      itemName="mining assets"
      hasPreviousPage
      hasNextPage={false}
      onPrevious={vi.fn()}
      onNext={vi.fn()}
    />,
  );
  expect(screen.getByText("No mining assets on this page")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Previous page" })).toBeEnabled();
  expect(screen.getByRole("button", { name: "Next page" })).toBeDisabled();
});
