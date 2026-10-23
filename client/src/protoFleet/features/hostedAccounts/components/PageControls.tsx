import Button from "@/shared/components/Button";

export const PageControls = ({
  previous,
  next,
  disabled = false,
  onPrevious,
  onNext,
}: {
  previous: boolean;
  next: boolean;
  /** Keeps the controls visible but inert, e.g. while a page loads. */
  disabled?: boolean;
  onPrevious: () => void;
  onNext: () => void;
}) =>
  previous || next ? (
    <div className="flex justify-end gap-3">
      <Button variant="secondary" text="Previous" disabled={disabled || !previous} onClick={onPrevious} />
      <Button variant="secondary" text="Next" disabled={disabled || !next} onClick={onNext} />
    </div>
  ) : null;
