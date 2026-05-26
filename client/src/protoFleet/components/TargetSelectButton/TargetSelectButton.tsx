import Button, { variants } from "@/shared/components/Button";
import Row from "@/shared/components/Row";

interface TargetSelectButtonProps {
  label: string;
  value: string;
  onClick: () => void;
}

function TargetSelectButton({ label, value, onClick }: TargetSelectButtonProps) {
  return (
    <Row className="flex min-h-[72px] items-center justify-between gap-4 py-5">
      <span className="min-w-0 truncate text-emphasis-300 text-text-primary">{label}</span>
      <Button ariaLabel={`${label} ${value}`} text={value} variant={variants.secondary} onClick={onClick} />
    </Row>
  );
}

export default TargetSelectButton;
