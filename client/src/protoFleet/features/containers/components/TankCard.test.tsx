import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import TankCard, { type TankCardProps } from "./TankCard";

const renderCard = (overrides: Partial<TankCardProps> = {}) => {
  const props: TankCardProps = {
    label: "Tank 1",
    cols: 1,
    rows: 1,
    modules: ["healthy"],
    on: true,
    onToggle: vi.fn(),
    stats: ["3/3 boards"],
    ...overrides,
  };

  render(<TankCard {...props} />);
  return props;
};

describe("TankCard", () => {
  it.each([
    [true, false],
    [false, true],
  ])("reports the next power state when on is %s", async (on, expected) => {
    const user = userEvent.setup();
    const onToggle = vi.fn();
    renderCard({ on, onToggle });

    await user.click(screen.getByRole("checkbox", { name: "Tank 1 power" }));

    expect(onToggle).toHaveBeenCalledWith(expected);
  });

  it.each(["{Enter}", " "])("selects the tank when activated with %s", async (key) => {
    const user = userEvent.setup();
    const onClick = vi.fn();
    renderCard({ onClick });

    screen.getByRole("button", { name: "View Tank 1" }).focus();
    await user.keyboard(key);

    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it("keeps keyboard power interaction separate from tank selection", async () => {
    const user = userEvent.setup();
    const onClick = vi.fn();
    const onToggle = vi.fn();
    renderCard({ onClick, onToggle });

    screen.getByRole("checkbox", { name: "Tank 1 power" }).focus();
    await user.keyboard(" ");

    expect(onToggle).toHaveBeenCalledWith(false);
    expect(onClick).not.toHaveBeenCalled();
  });

  it("keeps keyboard info interaction separate from tank selection", async () => {
    const user = userEvent.setup();
    const onClick = vi.fn();
    const onInfo = vi.fn();
    renderCard({ onClick, onInfo });

    screen.getByRole("button", { name: "Tank 1 info" }).focus();
    await user.keyboard("{Enter}");

    expect(onInfo).toHaveBeenCalledTimes(1);
    expect(onClick).not.toHaveBeenCalled();
  });
});
