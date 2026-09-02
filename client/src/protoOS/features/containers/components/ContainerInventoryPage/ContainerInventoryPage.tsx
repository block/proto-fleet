import Card, { cardType } from "@/shared/components/Card";

export interface ContainerInventoryItem {
  id: string;
  label: string;
  details: string[];
}

interface ContainerInventoryPageProps {
  emptyMessage: string;
  items: ContainerInventoryItem[];
  title: string;
}

const ContainerInventoryPage = ({ emptyMessage, items, title }: ContainerInventoryPageProps) => (
  <div className="flex flex-col gap-6" data-testid="container-inventory-page">
    <h1 className="text-heading-300">{title}</h1>
    {items.length ? (
      <div className="grid grid-cols-1 gap-4 tablet:grid-cols-2 desktop:grid-cols-3">
        {items.map((item) => (
          <Card
            key={item.id}
            title={<span className="text-emphasis-300">{item.label}</span>}
            type={cardType.default}
            headerTone="neutral"
            bodyClassName="flex flex-col gap-2 px-4 py-5"
            testId={`container-inventory-${item.id}`}
          >
            {item.details.map((detail) => (
              <p key={detail} className="text-300 text-text-primary-70">
                {detail}
              </p>
            ))}
          </Card>
        ))}
      </div>
    ) : (
      <p className="text-300 text-text-primary-70">{emptyMessage}</p>
    )}
  </div>
);

export default ContainerInventoryPage;
