import type { ReactNode } from "react";

import type { MinerAdapterId } from "@/protoOS/store/contract";
import { LogoAlt } from "@/shared/assets/icons";

interface UnsupportedMinerProps {
  adapterId?: MinerAdapterId;
  closeButton?: ReactNode | null;
}

const UnsupportedMiner = ({ adapterId, closeButton }: UnsupportedMinerProps) => {
  const description =
    adapterId === "mdk-v2"
      ? "MDK v2 was detected, but ProtoOS support is still being built."
      : "ProtoOS couldn't identify a supported miner API.";

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
          This miner isn't supported yet
        </h1>
        <p className="text-text-secondary text-400">{description}</p>
      </section>
    </main>
  );
};

export default UnsupportedMiner;
