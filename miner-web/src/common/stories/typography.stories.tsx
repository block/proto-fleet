interface BlockProps {
  label: string;
  typography: string;
}

const Block = ({ label, typography }: BlockProps) => {
  return (
    <div className="flex">
      <div className={`grow ${typography}`}>{label}</div>
      <div>{typography}</div>
    </div>
  );
};

export default {
  component: Block,
  title: "Typography",
};

export const Typography = () => {
  return (
    <div className="w-1/2 space-y-2">
      <Block typography="text-title-1" label="Title 1" />
      <Block typography="text-heading-300" label="Heading 300" />
      <Block typography="text-body-default" label="Body / Default" />
      <Block typography="text-button" label="Button" />
      <Block typography="text-body-regular" label="Body / Regular" />
    </div>
  );
};
