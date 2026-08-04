import Button, { sizes, variants } from "@/shared/components/Button";
import Row from "@/shared/components/Row";

interface TargetSelectButtonProps {
  label: string;
  value: string;
  disabled?: boolean;
  onClick: () => void;
  /**
   * Button size for the value control. Defaults to `base` to preserve the
   * curtailment/schedule modals' existing sizing; the firmware rollout Apply-to
   * tables opt into `compact`.
   */
  size?: keyof typeof sizes;
}

function TargetSelectButton({ label, value, disabled = false, onClick, size = sizes.base }: TargetSelectButtonProps) {
  return (
    <Row compact className="flex items-center justify-between gap-4">
      <span className="min-w-0 truncate text-emphasis-300 text-text-primary">{label}</span>
      <Button
        ariaLabel={`${label} ${value}`}
        text={value}
        variant={variants.secondary}
        size={size}
        disabled={disabled}
        onClick={onClick}
      />
    </Row>
  );
}

export default TargetSelectButton;
