import { useState } from "react";
import type { HealthScope } from "./scopeHealth";
import StatusModal from "./StatusModal";
import { useScopeHealth } from "./useScopeHealth";
import { Info } from "@/shared/assets/icons";

/** Tank/module entry point, derived from the same data as the modal. */
export default function HealthStatusButton({ scope }: { scope: HealthScope }) {
  const [open, setOpen] = useState(false);
  const summary = useScopeHealth(scope);
  const title = summary?.title ?? "Status unavailable";
  return (
    <>
      <button
        type="button"
        aria-haspopup="dialog"
        aria-label={`${summary?.label ?? "Container"} health: ${title}`}
        className="flex items-center gap-2 rounded-full bg-surface-10 px-3 py-2 text-300 text-text-primary hover:bg-surface-20 focus-visible:outline-2 focus-visible:outline-core-accent-fill"
        onClick={() => setOpen(true)}
      >
        <Info />
        {title}
      </button>
      {open ? <StatusModal scope={scope} onClose={() => setOpen(false)} /> : null}
    </>
  );
}
