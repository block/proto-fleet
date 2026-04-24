import Button from "@/shared/components/Button/Button";

export interface DefaultErrorFallbackProps {
  title: string;
  description?: string;
  error?: Error;
  showStackTrace?: boolean;
  onRetry: () => void;
}

const StackTrace = ({ error, className }: { error: unknown; className?: string }) => {
  if (!(error instanceof Error)) {
    return null;
  }

  const stackLines = error.stack?.split("\n");
  if (!stackLines) {
    return null;
  }

  const formatted = stackLines.map((line, index) => {
    return (
      <li key={index}>
        <code>{line.trim()}</code>
      </li>
    );
  });

  return <ul className={className}>{formatted}</ul>;
};

export const DefaultErrorFallback = ({
  title,
  description,
  error,
  showStackTrace = import.meta.env.DEV,
  onRetry,
}: DefaultErrorFallbackProps) => {
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
      {showStackTrace && error ? (
        <div className="flex flex-col gap-6 overflow-auto bg-zinc-900 p-6 text-yellow-400">
          <p className="text-mono-text-100">{error.message ?? "An unexpected error occurred"}</p>
          <StackTrace error={error} className="text-mono-text-100" />
        </div>
      ) : null}
    </div>
  );
};
