import { fireEvent, screen } from "@testing-library/react";

/**
 * Replaces `@/shared/components/Select` with a native `<select>` so tests
 * don't have to drive the production component's portal-mounted Popover and
 * focus-management dance (both of which misbehave in jsdom).
 *
 * Pass this factory to `vi.mock("@/shared/components/Select", selectStubModule)`.
 */
export const selectStubModule = () => ({
  __esModule: true,
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
    options: Array<{ value: string; label: string }>;
    value: string;
    onChange: (value: string) => void;
    disabled?: boolean;
  }) => (
    <label>
      {label}
      <select id={id} aria-label={label} value={value} onChange={(e) => onChange(e.target.value)} disabled={disabled}>
        <option value="" disabled hidden>
          {label}
        </option>
        {options.map((opt) => (
          <option key={opt.value} value={opt.value}>
            {opt.label}
          </option>
        ))}
      </select>
    </label>
  ),
});

/** Drives the (stubbed) Role select to choose the option labelled `roleLabel`. */
export const pickRole = (roleLabel: string) => {
  const select = screen.getByRole("combobox", { name: "Role" });
  const option = Array.from(select.querySelectorAll("option")).find((o) => o.textContent === roleLabel);
  if (!option) throw new Error(`Role option "${roleLabel}" not found in stubbed Select`);
  fireEvent.change(select, { target: { value: option.value } });
};
