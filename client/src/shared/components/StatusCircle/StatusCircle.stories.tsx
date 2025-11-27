import StatusCircleComponent, { type StatusCircleProps, statuses, variants } from ".";

export const StatusCircle = ({ status, variant, width }: StatusCircleProps) => {
  return <StatusCircleComponent status={status} width={width} variant={variant} />;
};

StatusCircle.args = {
  status: statuses.normal,
  variant: variants.primary,
  width: "w-4",
};

StatusCircle.argTypes = {
  status: {
    control: "select",
    options: Object.keys(statuses),
  },
  variant: {
    control: "select",
    options: Object.keys(variants),
  },
  width: {
    control: "select",
    options: ["w-2", "w-3", "w-4"],
  },
};

export default {
  title: "Shared/StatusCircle",
};
