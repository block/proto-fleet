import clsx from "clsx";

interface InfoWidgetProps {
  className?: string;
  title: string;
  value?: string;
}

const InfoWidget = ({ className, title, value }: InfoWidgetProps) => {
  return (
    <div className="min-w-[250px]">
      <div className="text-body-default font-semibold text-foreground-60 mb-2">
        {title}
      </div>
      <div className={clsx("text-title-1 font-normal leading-10 tracking-[-0.24px] font-mono", className)}>
        {value || "-"}
      </div>
    </div>
  );
};

export default InfoWidget;
