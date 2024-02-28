import Header from "components/Header";

interface ContentHeaderProps {
  subtitle: string;
  title: string;
}

const ContentHeader = ({ title, subtitle }: ContentHeaderProps) => {
  return (
    <Header
      title={title}
      subtitle={subtitle}
      titleSize="text-heading-300"
      subtitleSize="text-300"
      className="mb-10"
    />
  );
};

export default ContentHeader;
