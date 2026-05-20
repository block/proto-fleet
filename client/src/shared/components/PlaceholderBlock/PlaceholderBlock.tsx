import clsx from "clsx";

interface PlaceholderBlockProps {
  // Label shown inside the placeholder so reviewers and PR authors know what
  // the final component will be. Phase 1b replaces individual usages with
  // real components — see #263 / #264.
  label: string;
  // Tailwind sizing classes. Keep them on the wrapper so a placeholder can
  // pose as anything from a small metric tile to a full diagnostics grid.
  className?: string;
}

// FPO grey box used wherever Phase 1b will land richer content. Centralised
// so the visual treatment stays consistent across /sites and /buildings/:id
// and so the audit ("find every placeholder") is just a usage search.
const PlaceholderBlock = ({ label, className }: PlaceholderBlockProps) => (
  <div
    className={clsx(
      "flex items-center justify-center rounded-xl border border-dashed border-border-5 bg-surface-base text-200 text-text-primary-50",
      className,
    )}
    data-testid="placeholder-block"
  >
    {label}
  </div>
);

export default PlaceholderBlock;
