interface BlockProps {
  label: string;
  typography: string;
}

const Block = ({ label, typography }: BlockProps) => {
  return (
    <div className="flex items-center">
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
    <div className="space-y-2">
      <Block typography="text-heading-300" label="Heading 300" />
      <Block typography="text-heading-200" label="Heading 200" />
      <Block typography="text-400" label="Text 400" />
      <Block typography="text-300" label="Text 300" />
      <Block typography="text-200" label="Text 200" />
      <Block typography="text-emphasis-400" label="Text Emphasis 400" />
      <Block typography="text-emphasis-300" label="Text Emphasis 300" />
      <Block typography="text-emphasis-200" label="Text Emphasis 200" />
    </div>
  );
};
