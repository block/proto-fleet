import StatusCircle, {
  type StatusCircleProps,
} from "@/shared/components/StatusCircle";

type MinerStatusProps = {
  isSelected?: boolean;
  status: {
    hashboard: StatusCircleProps["status"];
    asic: StatusCircleProps["status"];
    fans: StatusCircleProps["status"];
    cb: StatusCircleProps["status"];
  };
};

const MinerStatus = ({ isSelected = false, status }: MinerStatusProps) => {
  return (
    <div className="flex flex-row opacity-70">
      <StatusCircle
        status={status.hashboard}
        variant="simple"
        width="w-[6px]"
        isSelected={isSelected}
      />
      <StatusCircle
        status={status.asic}
        variant="simple"
        width="w-[6px]"
        isSelected={isSelected}
      />
      <StatusCircle
        status={status.fans}
        variant="simple"
        width="w-[6px]"
        isSelected={isSelected}
      />
      <StatusCircle
        status={status.cb}
        variant="simple"
        width="w-[6px]"
        isSelected={isSelected}
      />
    </div>
  );
};

export default MinerStatus;
