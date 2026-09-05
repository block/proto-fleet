import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import ListPagination from "./ListPagination";

it("shows the current result range and exposes horizontal page controls", () => {
  const previous = vi.fn();
  const next = vi.fn();
  render(
    <ListPagination
      currentPage={1}
      pageSize={50}
      visibleCount={25}
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
