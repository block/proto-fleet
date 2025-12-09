import SectionHeadingComponent from ".";
import Button, { sizes, variants } from "@/shared/components/Button";
import DurationSelector from "@/shared/components/DurationSelector";

interface SectionHeadingArgs {
  heading: string;
  controlType: "none" | "durationSelector" | "button";
}

export const SectionHeading = ({ heading, controlType }: SectionHeadingArgs) => {
  const renderControls = () => {
    switch (controlType) {
      case "durationSelector":
        return <DurationSelector />;
      case "button":
        return <Button variant={variants.secondary} size={sizes.compact} text="Manage fleet" />;
      case "none":
      default:
        return null;
    }
  };

  return (
    <div className="bg-surface-5 p-10 dark:bg-surface-base">
      <SectionHeadingComponent heading={heading}>{renderControls()}</SectionHeadingComponent>
    </div>
  );
};

export default {
  title: "Proto Fleet/Dashboard/SectionHeading",
  args: {
    heading: "Overview",
    controlType: "button",
  },
  argTypes: {
    heading: {
      control: "text",
    },
    controlType: {
      control: "select",
      options: ["none", "durationSelector", "button"],
      description: "Type of control to display on the right side",
    },
  },
};
