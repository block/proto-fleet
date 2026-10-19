import Button from "@/shared/components/Button";

export const PageControls = ({
  previous,
  next,
  onPrevious,
  onNext,
}: {
  previous: boolean;
  next: boolean;
  onPrevious: () => void;
  onNext: () => void;
}) =>
  previous || next ? (
    <div className="flex justify-end gap-3">
      <Button variant="secondary" text="Previous" disabled={!previous} onClick={onPrevious} />
      <Button variant="secondary" text="Next" disabled={!next} onClick={onNext} />
    </div>
  ) : null;
