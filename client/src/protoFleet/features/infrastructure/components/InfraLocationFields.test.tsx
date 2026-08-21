import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";

import InfraLocationFields from "./InfraLocationFields";

vi.mock("@/shared/components/Select", () => ({
  default: ({
    id,
    label,
    options,
    value,
    onChange,
    disabled,
  }: {
    id: string;
    label: string;
    options: { value: string; label: string }[];
    value: string;
    onChange: (value: string) => void;
    disabled?: boolean;
  }) => (
    <label htmlFor={id}>
      {label}
      <select id={id} aria-label={label} value={value} disabled={disabled} onChange={(e) => onChange(e.target.value)}>
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </label>
  ),
}));

describe("InfraLocationFields", () => {
  test("disables selectors when no location options are available", () => {
    render(
      <InfraLocationFields
        site=""
        building=""
        rack=""
        siteOptions={[]}
        buildingOptions={[]}
        rackOptions={[]}
        onSiteChange={vi.fn()}
        onBuildingChange={vi.fn()}
        onRackChange={vi.fn()}
      />,
    );

    expect(screen.getByRole("combobox", { name: "Site" })).toBeDisabled();
    expect(screen.getByRole("combobox", { name: "Building" })).toBeDisabled();
    expect(screen.getByRole("combobox", { name: "Rack" })).toBeDisabled();
  });

  test("preserves existing location values as selector options", () => {
    render(
      <InfraLocationFields
        site="Legacy site"
        building="Legacy building"
        rack="Legacy rack"
        siteOptions={[]}
        buildingOptions={[]}
        rackOptions={[]}
        onSiteChange={vi.fn()}
        onBuildingChange={vi.fn()}
        onRackChange={vi.fn()}
      />,
    );

    expect(screen.getByRole("combobox", { name: "Site" })).toHaveValue("Legacy site");
    expect(screen.getByRole("combobox", { name: "Building" })).toHaveValue("Legacy building");
    expect(screen.getByRole("combobox", { name: "Rack" })).toHaveValue("Legacy rack");
  });

  test("offers an explicit unracked option when rack choices are available", () => {
    const onRackChange = vi.fn();
    render(
      <InfraLocationFields
        site="Austin"
        building="Building 1"
        rack="Rack A1"
        siteOptions={["Austin"]}
        buildingOptions={[{ siteName: "Austin", buildingName: "Building 1" }]}
        rackOptions={[{ siteName: "Austin", buildingName: "Building 1", rackName: "Rack A1" }]}
        onSiteChange={vi.fn()}
        onBuildingChange={vi.fn()}
        onRackChange={onRackChange}
      />,
    );

    const rackSelect = screen.getByRole("combobox", { name: "Rack" });
    expect(screen.getByRole("option", { name: "No rack" })).toHaveValue("");
    fireEvent.change(rackSelect, { target: { value: "" } });
    expect(onRackChange).toHaveBeenCalledWith("");
  });

  test("keeps an unracked device unracked when its location changes", () => {
    const onBuildingChange = vi.fn();
    const onRackChange = vi.fn();
    render(
      <InfraLocationFields
        site="Austin"
        building="Building 1"
        rack=""
        siteOptions={["Austin", "Denver"]}
        buildingOptions={[
          { siteName: "Austin", buildingName: "Building 1" },
          { siteName: "Denver", buildingName: "Denver Plant" },
        ]}
        rackOptions={[
          { siteName: "Austin", buildingName: "Building 1", rackName: "Rack A1" },
          { siteName: "Denver", buildingName: "Denver Plant", rackName: "Rack D1" },
        ]}
        onSiteChange={vi.fn()}
        onBuildingChange={onBuildingChange}
        onRackChange={onRackChange}
      />,
    );

    fireEvent.change(screen.getByRole("combobox", { name: "Site" }), { target: { value: "Denver" } });
    expect(onBuildingChange).toHaveBeenCalledWith("Denver Plant");
    expect(onRackChange).not.toHaveBeenCalled();
  });
});
