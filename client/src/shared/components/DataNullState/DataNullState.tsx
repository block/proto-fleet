import Button from "../Button/Button";

export interface DataNullStateProps {
  title: string;
  description: string;
  onRetry?: () => void;
}

export const DataNullState = ({ title, description, onRetry }: DataNullStateProps) => {
  return (
    <div className="flex flex-col items-center justify-center gap-6 pt-40">
      <div className="flex flex-col items-center justify-center gap-1">
        <p className="text-heading-200">{title}</p>
        {description ? <p className="text-400">{description}</p> : null}
      </div>

      {onRetry ? (
        <Button variant="secondary" onClick={onRetry}>
          Retry
        </Button>
      ) : null}
    </div>
  );
};

export default DataNullState;
