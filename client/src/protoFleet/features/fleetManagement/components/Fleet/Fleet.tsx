import clsx from "clsx";
import useFleet from "@/protoFleet/api/useFleet";
import MinerList from "@/protoFleet/features/fleetManagement/components/MinerList";

const Fleet = () => {
  const { minerIds } = useFleet();
  return (
    <MinerList
      title="Miners"
      minerIds={minerIds}
      listClassName={clsx(
        // limit the height of the list to activate sticky header
        // take height of the screen - top and bottom paddings - page header - miner list header
        "phone:max-h-[calc(100vh-theme(spacing.6)*2-(theme(spacing.12)+57px)-theme(spacing.10))]",
        "tablet:max-h-[calc(100vh-theme(spacing.6)*2-theme(spacing.12)-theme(spacing.10))]",
        "laptop:max-h-[calc(100vh-theme(spacing.20)*2-(theme(spacing.14)+theme(spacing.1))-theme(spacing.10))]",
        "desktop:max-h-[calc(100vh-theme(spacing.20)*2-(theme(spacing.14)+theme(spacing.1))-theme(spacing.10))]",
      )}
      // theme(spacing.20) doesn't work here because this is not preprocessed by Tailwind
      paddingLeft={{
        phone: "24px",
        tablet: "24px",
        // Left padding is padding of the parent (container) + half of auto margin caused by justify center
        laptop: "calc(80px + (100vw - 776px - 80px * 2 - 64px)/2)",
        desktop: "calc(80px + (100vw - 1024px - 80px * 2 - 64px)/2)",
      }}
      overflowContainer={true}
    />
  );
};

export default Fleet;
