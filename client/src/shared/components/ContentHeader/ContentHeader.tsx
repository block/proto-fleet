import Header from "@/shared/components/Header";

interface ContentHeaderProps {
  subtitle: string;
  testId?: string;
  title: string;
}

const ContentHeader = ({ subtitle, testId, title }: ContentHeaderProps) => {
  return (
    <Header
      title={title}
      subtitle={subtitle}
      titleSize="text-heading-300"
      subtitleSize="text-300"
      className="mb-10"
      testId={testId}
    />
  );
};

export default ContentHeader;
