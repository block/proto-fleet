import clsx from "clsx";
import useFleet from "@/protoFleet/api/useFleet";
import MinerList from "@/protoFleet/features/fleetManagement/components/MinerList";

const Fleet = () => {
  const { miners } = useFleet();
  return (
    <MinerList
      title="Miners"
      miners={miners}
      bodyClassName={clsx(
        // Take width of the parent, add left and right padding and auto margin caused by justify center
        // In the end subtract one spacing unit to account for minor inaccuracy in the calculation
        // Auto padding (left and right) is computed like this: screen width - parent (container width) - left and right paddings - left navigation
        "phone:w-[calc(100%+theme(spacing.6)*2+(100vw-100%-theme(spacing.6)*2)-theme(spacing.1))]",
        "tablet:w-[calc(100%+theme(spacing.6)*2+(100vw-100%-theme(spacing.6)*2)-theme(spacing.1))]",
        "laptop:w-[calc(100%+theme(spacing.20)*2+(100vw-100%-theme(spacing.20)*2-theme(spacing.16))-theme(spacing.1))]",
        "desktop:w-[calc(100%+theme(spacing.20)*2+(100vw-100%-theme(spacing.20)*2-theme(spacing.16))-theme(spacing.1))]",
        // Left padding is padding of the parent (container) + half of auto margin caused by justify center
        "phone:px-[calc(theme(spacing.6)+(100vw-100%-theme(spacing.6)*2)/2)]",
        "tablet:px-[calc(theme(spacing.6)+(100vw-100%-theme(spacing.6)*2)/2)]",
        "laptop:px-[calc(theme(spacing.20)+(100vw-100%-theme(spacing.20)*2-theme(spacing.16))/2)]",
        "desktop:px-[calc(theme(spacing.20)+(100vw-100%-theme(spacing.20)*2-theme(spacing.16))/2)]",
        // Translate the element left by the padding
        "phone:-translate-x-[calc(theme(spacing.6)+(100vw-352px-theme(spacing.6)*2)/2))]",
        "tablet:-translate-x-[calc(theme(spacing.6)+(100vw-584px-theme(spacing.6)*2)/2))]",
        "laptop:-translate-x-[calc(theme(spacing.20)+(100vw-776px-theme(spacing.20)*2-theme(spacing.16))/2))]",
        "desktop:-translate-x-[calc(theme(spacing.20)+(100vw-1024px-theme(spacing.20)*2-theme(spacing.16))/2))]",
        "overflow-x-auto",
      )}
    />
  );
};

export default Fleet;
