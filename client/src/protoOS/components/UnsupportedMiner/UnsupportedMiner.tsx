import type { ReactNode } from "react";

import type { MinerAdapterId } from "@/protoOS/store/contract";
import { LogoAlt } from "@/shared/assets/icons";
import Button, { variants } from "@/shared/components/Button";

interface UnsupportedMinerProps {
  adapterId?: MinerAdapterId;
  closeButton?: ReactNode | null;
  // When provided, this is the recoverable "couldn't reach the miner" state
  // rather than a terminal "unsupported miner" one: a distinct message and a
  // retry button that re-runs the probe. Re-probing is state-driven, so the
  // button needs no navigation (no minerRoot prefix required).
  onRetry?: () => void;
}

const UnsupportedMiner = ({ adapterId, closeButton, onRetry }: UnsupportedMinerProps) => {
  const { title, description } = onRetry
    ? {
        title: "Couldn't reach this miner",
        description: "ProtoOS couldn't reach the miner to identify it. Check that it's online and try again.",
      }
    : {
        title: "This miner isn't supported yet",
        description:
          adapterId === "mdk-v2"
            ? "MDK v2 was detected, but ProtoOS support is still being built."
            : "ProtoOS couldn't identify a supported miner API.",
      };

  return (
    <main
      className="relative flex min-h-screen items-center justify-center bg-surface-base px-6"
      data-testid="unsupported-miner-page"
    >
      {closeButton ? <div className="absolute top-6 left-6">{closeButton}</div> : null}
      <section
        className="flex max-w-xl flex-col items-center gap-4 text-center"
        aria-labelledby="unsupported-miner-title"
      >
        <LogoAlt width="w-16" />
        <h1 id="unsupported-miner-title" className="text-heading-300 text-text-primary">
          {title}
        </h1>
        <p className="text-text-secondary text-400">{description}</p>
        {onRetry ? <Button variant={variants.primary} text="Try again" onClick={onRetry} testId="retry-probe" /> : null}
      </section>
    </main>
  );
};

export default UnsupportedMiner;
