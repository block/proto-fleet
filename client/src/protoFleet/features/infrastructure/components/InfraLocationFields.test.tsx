import { useState } from "react";
import { render, screen } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import userEvent from "@testing-library/user-event";

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

const LocationFieldsHarness = ({ catalogReady }: { catalogReady: boolean }) => {
  const [site, setSite] = useState("");
  const [building, setBuilding] = useState("");

  return (
    <InfraLocationFields
      site={site}
      building={building}
      siteOptions={catalogReady ? ["Austin"] : []}
      buildingOptions={[]}
      onSiteChange={setSite}
      onBuildingChange={setBuilding}
      allowCustomValues
    />
  );
};

describe("InfraLocationFields", () => {
  test("preserves custom building text when catalog options arrive", async () => {
    const user = userEvent.setup();
    const { rerender } = render(<LocationFieldsHarness catalogReady={false} />);

    await user.type(screen.getByLabelText("Site"), "Austin");
    await user.type(screen.getByLabelText("Building"), "Warehouse X");

    expect(screen.getByLabelText("Building")).toHaveValue("Warehouse X");

    rerender(<LocationFieldsHarness catalogReady />);

    expect(screen.getByLabelText("Building")).toHaveValue("Warehouse X");
  });
});
