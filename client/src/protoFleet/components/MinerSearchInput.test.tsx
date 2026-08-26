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
