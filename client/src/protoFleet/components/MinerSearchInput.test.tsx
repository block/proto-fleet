import { useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import MinerSearchInput from "./MinerSearchInput";

const SEARCH_DEBOUNCE_MS = 250;

/** Mirrors the miner list: the emitted query is persisted (there, to the URL)
 * and handed straight back as `initialValue`. That round-trip is what makes a
 * normalized emit overwrite the text the operator is still typing. */
const RoundTripHarness = ({ onQueryChange }: { onQueryChange?: (q: string) => void } = {}) => {
  const [persisted, setPersisted] = useState("");
  return (
    <MinerSearchInput
      initialValue={persisted}
      onQueryChange={(query) => {
        onQueryChange?.(query);
        setPersisted(query);
      }}
    />
  );
};

const searchBox = () => screen.getByRole("textbox", { name: /search miners/i });

describe("MinerSearchInput", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("keeps a trailing space through the round-trip so multi-word queries stay typable", () => {
    vi.useFakeTimers();
    render(<RoundTripHarness />);

    // Pausing mid-query is the trigger: the debounce fires, the value is
    // persisted, and it comes back as initialValue while the field still has
    // focus. Trimming on the way out deleted the space, turning the next
    // keystroke into "rack7".
    fireEvent.change(searchBox(), { target: { value: "rack " } });
    vi.advanceTimersByTime(SEARCH_DEBOUNCE_MS);

    expect(searchBox()).toHaveValue("rack ");

    fireEvent.change(searchBox(), { target: { value: "rack 7" } });
    vi.advanceTimersByTime(SEARCH_DEBOUNCE_MS);

    expect(searchBox()).toHaveValue("rack 7");
  });

  it("emits the query as typed rather than a normalized form", () => {
    vi.useFakeTimers();
    const onQueryChange = vi.fn();
    render(<RoundTripHarness onQueryChange={onQueryChange} />);

    fireEvent.change(searchBox(), { target: { value: "rack 7 " } });
    vi.advanceTimersByTime(SEARCH_DEBOUNCE_MS);

    expect(onQueryChange).toHaveBeenCalledWith("rack 7 ");
  });

  it("drops leading whitespace from both the field and the emitted query", () => {
    vi.useFakeTimers();
    const onQueryChange = vi.fn();
    render(<RoundTripHarness onQueryChange={onQueryChange} />);

    fireEvent.change(searchBox(), { target: { value: "  rack" } });

    // Applied at the input, so the visible text and the emitted value agree and
    // the echo cannot rewrite the field.
    expect(searchBox()).toHaveValue("rack");

    vi.advanceTimersByTime(SEARCH_DEBOUNCE_MS);
    expect(onQueryChange).toHaveBeenCalledWith("rack");
  });

  it("issues one query per typing burst rather than one per keystroke", () => {
    vi.useFakeTimers();
    const onQueryChange = vi.fn();
    render(<RoundTripHarness onQueryChange={onQueryChange} />);

    fireEvent.change(searchBox(), { target: { value: "r" } });
    fireEvent.change(searchBox(), { target: { value: "ra" } });
    fireEvent.change(searchBox(), { target: { value: "rack" } });
    expect(onQueryChange).not.toHaveBeenCalled();

    vi.advanceTimersByTime(SEARCH_DEBOUNCE_MS);
    expect(onQueryChange).toHaveBeenCalledExactlyOnceWith("rack");
  });

  it("reports typing synchronously so safety gates do not wait for the debounce", () => {
    vi.useFakeTimers();
    const onQueryChange = vi.fn();
    const onQueryInput = vi.fn();
    render(<MinerSearchInput initialValue="" onQueryChange={onQueryChange} onQueryInput={onQueryInput} />);

    fireEvent.change(searchBox(), { target: { value: "rack" } });

    // Consumers gate destructive all-mode selections on "is a search active".
    // If that only became true after the debounce, an all-mode action submitted
    // inside the window would apply to the whole fleet while the field already
    // showed a query.
    expect(onQueryInput).toHaveBeenCalledExactlyOnceWith("rack");
    expect(onQueryChange).not.toHaveBeenCalled();

    vi.advanceTimersByTime(SEARCH_DEBOUNCE_MS);
    expect(onQueryChange).toHaveBeenCalledExactlyOnceWith("rack");
  });

  it("reports every keystroke synchronously, not just the first", () => {
    vi.useFakeTimers();
    const onQueryInput = vi.fn();
    render(<MinerSearchInput initialValue="" onQueryChange={vi.fn()} onQueryInput={onQueryInput} />);

    fireEvent.change(searchBox(), { target: { value: "r" } });
    fireEvent.change(searchBox(), { target: { value: "ra" } });
    fireEvent.change(searchBox(), { target: { value: "" } });

    // Clearing has to report too, or the gate would stay latched shut.
    expect(onQueryInput.mock.calls.map(([q]) => q)).toEqual(["r", "ra", ""]);
  });

  it("cancels a pending query when an external value replaces it", () => {
    vi.useFakeTimers();
    const onQueryChange = vi.fn();
    const onQueryInput = vi.fn();
    const { rerender } = render(
      <MinerSearchInput initialValue="" onQueryChange={onQueryChange} onQueryInput={onQueryInput} />,
    );

    fireEvent.change(searchBox(), { target: { value: "rack" } });
    rerender(<MinerSearchInput initialValue="saved-view" onQueryChange={onQueryChange} onQueryInput={onQueryInput} />);
    vi.advanceTimersByTime(SEARCH_DEBOUNCE_MS);

    expect(searchBox()).toHaveValue("saved-view");
    expect(onQueryChange).not.toHaveBeenCalled();
    expect(onQueryInput).toHaveBeenLastCalledWith("saved-view");
  });

  it("caps search text at the API's 255 Unicode code-point limit", () => {
    vi.useFakeTimers();
    const onQueryChange = vi.fn();
    render(<MinerSearchInput initialValue="" onQueryChange={onQueryChange} />);

    const query = `${"a".repeat(254)}🐝extra`;
    fireEvent.change(searchBox(), { target: { value: query } });
    vi.advanceTimersByTime(SEARCH_DEBOUNCE_MS);

    const expected = `${"a".repeat(254)}🐝`;
    expect(searchBox()).toHaveValue(expected);
    expect(onQueryChange).toHaveBeenCalledExactlyOnceWith(expected);
  });

  it("cancels a pending query when unmounted mid-debounce", () => {
    vi.useFakeTimers();
    const onQueryChange = vi.fn();
    const { unmount } = render(<RoundTripHarness onQueryChange={onQueryChange} />);

    fireEvent.change(searchBox(), { target: { value: "rack" } });
    unmount();
    vi.advanceTimersByTime(SEARCH_DEBOUNCE_MS);

    expect(onQueryChange).not.toHaveBeenCalled();
  });
});
