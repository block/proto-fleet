import Header from "@/shared/components/Header";

type SettingsPageHeaderProps = {
  title: string;
  description?: string;
};

const SettingsPageHeader = ({ title, description }: SettingsPageHeaderProps) => (
  <Header title={title} titleSize="text-heading-300" description={description} />
);

export default SettingsPageHeader;
