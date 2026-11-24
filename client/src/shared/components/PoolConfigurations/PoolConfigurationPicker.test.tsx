import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { expect, test, vi } from "vitest";
import { PoolConfigurationPicker } from "./PoolConfigurationPicker";
import { ActionModal } from "@/shared/components/ActionModal";

test("shows multiple backup pools from the info icon without selecting the configuration", async () => {
  const primary = "stratum+tcp://primary.example:3333";
  const backups = ["stratum+tcp://backup1.example:3333", "stratum+tcp://backup2.example:3333"];
  const onSelect = vi.fn();
  const onDismiss = vi.fn();
  render(
    <ActionModal title="Edit pool" confirm="Assign" onSubmit={vi.fn()} onDismiss={onDismiss}>
      <PoolConfigurationPicker
        selectionDescription="1 hashboard selected."
        loading={false}
        onRetry={vi.fn()}
        configurations={[
          { configurationId: "default", name: "Default", pools: [primary, ...backups].map((url) => ({ url })) },
        ]}
        onSelect={onSelect}
      />
    </ActionModal>,
  );

  await waitFor(() => expect(screen.getByText("2 pools")).toBeVisible());
  const info = screen.getByRole("button", { name: "Backup pools for Default" });
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();

  vi.useFakeTimers();
  try {
    fireEvent.mouseEnter(info);
    const tooltip = screen.getByRole("tooltip");
    expect(
      within(tooltip)
        .getAllByRole("listitem")
        .map((item) => item.textContent),
    ).toEqual(backups);
    fireEvent.mouseLeave(info);
    act(() => vi.advanceTimersByTime(50));
    expect(tooltip).toBeVisible();
    fireEvent.mouseEnter(tooltip);
    act(() => vi.advanceTimersByTime(200));
    expect(tooltip).toBeVisible();
    fireEvent.mouseDown(within(tooltip).getByText(backups[0]));
    expect(tooltip).toBeVisible();
    fireEvent.mouseLeave(tooltip);
    act(() => vi.advanceTimersByTime(200));
    expect(tooltip).not.toBeInTheDocument();

    fireEvent.focus(info);
    expect(screen.getByRole("tooltip")).toBeVisible();
    fireEvent.mouseEnter(info);
    fireEvent.mouseLeave(info);
    act(() => vi.advanceTimersByTime(200));
    expect(screen.getByRole("tooltip")).toBeVisible();
    fireEvent.blur(info);
    act(() => vi.advanceTimersByTime(200));
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();

    fireEvent.click(info);
    expect(screen.getByRole("tooltip")).toBeVisible();
    fireEvent.mouseDown(screen.getByText("1 hashboard selected."));
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
    expect(onSelect).not.toHaveBeenCalled();

    fireEvent.focus(info);
    expect(screen.getByRole("tooltip")).toBeVisible();
    fireEvent.keyDown(info, { key: "Escape" });
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
    expect(onDismiss).not.toHaveBeenCalled();
    fireEvent.keyDown(info, { key: "Escape" });
    expect(onDismiss).toHaveBeenCalledTimes(1);
  } finally {
    vi.useRealTimers();
  }
});
