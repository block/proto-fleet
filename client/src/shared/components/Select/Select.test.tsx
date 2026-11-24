import { useState } from "react";
import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import Select from "./Select";
import Search from "@/shared/components/Search";

let intersect: (visible: boolean) => void;
const observe = vi.fn();
const disconnect = vi.fn();
const observerOptions = vi.fn();
beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 40));
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      constructor(
        callback: ConstructorParameters<typeof IntersectionObserver>[0],
        options: ConstructorParameters<typeof IntersectionObserver>[1],
      ) {
        observerOptions(options);
        intersect = (visible) =>
          callback([{ isIntersecting: visible } as IntersectionObserverEntry], this as unknown as IntersectionObserver);
      }
      observe = observe;
      disconnect = disconnect;
    },
  );
  vi.clearAllMocks();
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

const options = [
  { value: "a", label: "Board A" },
  { value: "b", label: "Board B" },
];
const props = { id: "board", label: "Board", value: "", options, onChange: vi.fn() };

describe("Select", () => {
  it("supports ordinary selection and returns focus to the field", async () => {
    const user = userEvent.setup();
    render(<Select {...props} />);
    await user.click(screen.getByRole("button", { name: "Board" }));
    await user.click(screen.getByRole("option", { name: "Board B" }));
    expect(props.onChange).toHaveBeenCalledWith("b");
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Board" })).toHaveFocus();
    expect(observe).not.toHaveBeenCalled();
  });

  it("keeps search outside the listbox and supports keyboard selection", async () => {
    const user = userEvent.setup();
    const Picker = () => {
      const [search, setSearch] = useState("");
      return (
        <Select
          {...props}
          options={options.filter((option) => option.label.includes(search))}
          search={
            <Search id="search" label="Search boards" variant="toolbar" initValue={search} onChange={setSearch} />
          }
        />
      );
    };
    render(<Picker />);
    await user.click(screen.getByRole("button", { name: "Board" }));
    const input = screen.getByRole("textbox", { name: "Search boards" });
    expect(input).toHaveFocus();
    expect(screen.getByRole("listbox")).not.toContainElement(input);
    await user.type(input, "B");
    await user.keyboard("{Home}{End}");
    expect(input).toHaveFocus();
    await user.keyboard("{ArrowUp}{ArrowDown}");
    expect(screen.getByRole("option", { name: "Board B" })).toHaveFocus();
    await user.keyboard("{Home}{ArrowUp}");
    expect(screen.getByRole("option", { name: "Board A" })).toHaveFocus();
    await user.keyboard("{End}");
    expect(screen.getByRole("option", { name: "Board B" })).toHaveFocus();
    await user.keyboard("{ArrowUp}{ArrowDown}{Enter}");
    expect(props.onChange).toHaveBeenCalledWith("b");
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  });

  it("loads once near the end and waits for the current page before observing again", async () => {
    const user = userEvent.setup();
    const loadMore = vi.fn();
    const { rerender } = render(<Select {...props} hasMore onLoadMore={loadMore} />);
    await user.click(screen.getByRole("button", { name: "Board" }));
    expect(observerOptions).toHaveBeenLastCalledWith(expect.objectContaining({ root: screen.getByRole("listbox") }));
    act(() => intersect(false));
    expect(loadMore).not.toHaveBeenCalled();
    act(() => {
      intersect(true);
      intersect(true);
    });
    expect(loadMore).toHaveBeenCalledOnce();
    rerender(<Select {...props} hasMore onLoadMore={loadMore} isLoading />);
    expect(screen.getByRole("status")).toHaveTextContent("Loading");
    expect(screen.getByRole("option", { name: "Board A" })).toBeInTheDocument();
    act(() => intersect(true));
    expect(loadMore).toHaveBeenCalledOnce();
    rerender(<Select {...props} hasMore onLoadMore={loadMore} />);
    act(() => intersect(true));
    expect(loadMore).toHaveBeenCalledTimes(2);
    rerender(<Select {...props} onLoadMore={loadMore} />);
    act(() => intersect(true));
    expect(loadMore).toHaveBeenCalledTimes(2);
  });

  it("offers retry after a page failure without loading more automatically", async () => {
    const user = userEvent.setup();
    const retry = vi.fn();
    render(<Select {...props} hasMore onLoadMore={vi.fn()} loadError="Could not load boards" onRetry={retry} />);
    await user.click(screen.getByRole("button", { name: "Board" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Could not load boards");
    expect(observe).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Retry" }));
    expect(retry).toHaveBeenCalledOnce();
  });

  it("shows empty results and closes with Escape", async () => {
    const user = userEvent.setup();
    render(
      <Select
        {...props}
        options={[]}
        emptyMessage="No matching boards"
        search={<Search id="search" label="Search boards" onChange={vi.fn()} />}
      />,
    );
    await user.click(screen.getByRole("button", { name: "Board" }));
    expect(screen.getByText("No matching boards")).toBeInTheDocument();
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Board" })).toHaveFocus();
  });
});
