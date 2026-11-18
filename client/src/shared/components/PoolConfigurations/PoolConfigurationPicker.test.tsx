import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
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

  fireEvent.mouseEnter(info);
  const tooltip = screen.getByRole("tooltip");
  expect(
    within(tooltip)
      .getAllByRole("listitem")
      .map((item) => item.textContent),
  ).toEqual(backups);
  fireEvent.mouseLeave(info);
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();

  fireEvent.focus(info);
  expect(screen.getByRole("tooltip")).toBeVisible();
  fireEvent.blur(info);
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();

  fireEvent.click(info);
  expect(screen.getByRole("tooltip")).toBeVisible();
  expect(onSelect).not.toHaveBeenCalled();
  fireEvent.keyDown(info, { key: "Escape" });
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  expect(onDismiss).not.toHaveBeenCalled();
  fireEvent.keyDown(info, { key: "Escape" });
  expect(onDismiss).toHaveBeenCalledTimes(1);
});
