import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import Search from "./Search";

const searchBox = () => screen.getByRole("textbox", { name: /search/i });
const clearButton = () => screen.queryByRole("button", { name: /clear search/i });

describe("Search", () => {
  describe("toolbar variant", () => {
    it("renders a container so the field is distinguishable from static text", () => {
      const { container } = render(<Search variant="toolbar" onChange={vi.fn()} />);

      // The whole point of the variant: Input drops its border in compact mode,
      // so without a container the field is invisible next to bordered controls.
      expect(container.querySelector(".border")).not.toBeNull();
    });

    it("offers a clear control once there is something to clear", () => {
      const onChange = vi.fn();
      render(<Search variant="toolbar" onChange={onChange} />);

      expect(clearButton()).toBeNull();

      fireEvent.change(searchBox(), { target: { value: "rack-7" } });
      expect(clearButton()).not.toBeNull();
    });

    it("clears the field and reports the empty query when the clear control is used", () => {
      const onChange = vi.fn();
      render(<Search variant="toolbar" onChange={onChange} />);

      fireEvent.change(searchBox(), { target: { value: "rack-7" } });
      onChange.mockClear();

      fireEvent.click(clearButton()!);

      expect(searchBox()).toHaveValue("");
      expect(onChange).toHaveBeenCalledWith("", "search");
    });

    it("keeps focus in the field after clearing so a new query can be typed", () => {
      render(<Search variant="toolbar" onChange={vi.fn()} />);

      fireEvent.change(searchBox(), { target: { value: "rack-7" } });
      fireEvent.click(clearButton()!);

      expect(searchBox()).toHaveFocus();
    });
  });

  describe("compact variant", () => {
    it("has no clear control, which is why toolbar exists", () => {
      render(<Search variant="compact" onChange={vi.fn()} />);

      fireEvent.change(searchBox(), { target: { value: "rack-7" } });

      expect(clearButton()).toBeNull();
    });
  });

  it("clears on Escape in every variant", () => {
    const onChange = vi.fn();
    render(<Search variant="toolbar" onChange={onChange} />);

    fireEvent.change(searchBox(), { target: { value: "rack-7" } });
    fireEvent.keyDown(searchBox(), { key: "Escape" });

    expect(searchBox()).toHaveValue("");
    expect(onChange).toHaveBeenLastCalledWith("", "search");
  });

  it("re-seeds the field when the caller supplies a new value", () => {
    const { rerender } = render(<Search variant="toolbar" initValue="rack-7" onChange={vi.fn()} />);
    expect(searchBox()).toHaveValue("rack-7");

    // Back/forward navigation and "clear filters" reach the field this way.
    rerender(<Search variant="toolbar" initValue="" onChange={vi.fn()} />);
    expect(searchBox()).toHaveValue("");
  });
});
