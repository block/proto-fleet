interface TargetSelectButtonProps {
  label: string;
  value: string;
  onClick: () => void;
}

function TargetSelectButton({ label, value, onClick }: TargetSelectButtonProps) {
  return (
    <button
      type="button"
      aria-label={`${label} ${value}`}
      onClick={onClick}
      className="flex min-h-[72px] w-full items-center justify-between gap-4 border-b border-border-5 py-5 text-left outline-hidden transition-colors hover:bg-core-primary-5 focus-visible:ring-4 focus-visible:ring-core-primary-20"
    >
      <span className="min-w-0 truncate text-emphasis-300 text-text-primary">{label}</span>
      <span className="max-w-[50%] shrink-0 truncate rounded-full bg-surface-overlay px-5 py-2 text-emphasis-300 text-text-primary">
        {value}
      </span>
    </button>
  );
}

export default TargetSelectButton;
