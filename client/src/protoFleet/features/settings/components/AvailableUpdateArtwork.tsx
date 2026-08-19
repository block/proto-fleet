import { LogoAlt } from "@/shared/assets/icons";

const AvailableUpdateArtwork = () => (
  <div
    aria-hidden="true"
    data-testid="available-update-artwork"
    className="relative flex h-[240px] min-h-[240px] w-full shrink-0 items-center justify-center overflow-hidden bg-transparent tablet:h-auto tablet:w-[30%] tablet:self-stretch"
  >
    <div className="absolute size-32 rounded-full bg-core-primary-fill opacity-10 blur-3xl motion-safe:animate-pulse" />
    <LogoAlt
      width="w-24"
      className="relative text-core-primary-fill opacity-15 drop-shadow-[0_0_24px_currentColor] motion-safe:animate-pulse"
    />
  </div>
);

export default AvailableUpdateArtwork;
