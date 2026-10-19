import { useState } from "react";
import type { HealthScope } from "./scopeHealth";
import StatusModal from "./StatusModal";
import { useScopeHealth } from "./useScopeHealth";
import StatusPill from "@/shared/components/StatusPill";

/** Tank/module entry point, derived from the same data as the modal. */
export default function HealthStatusButton({ scope }: { scope: HealthScope }) {
  const [open, setOpen] = useState(false);
  const summary = useScopeHealth(scope);
  const title = summary?.title ?? "Status unavailable";
  return (
    <>
      <StatusPill
        ariaLabel={`${summary?.label ?? "Container"} health: ${title}`}
        summary={title}
        circle={
          summary?.needsAttention
            ? "error"
            : title === "All components operational"
              ? "normal"
              : title === "Sleeping"
                ? "sleeping"
                : "inactive"
        }
        onClick={() => setOpen(true)}
      />
      {open ? <StatusModal scope={scope} onClose={() => setOpen(false)} /> : null}
    </>
  );
}
