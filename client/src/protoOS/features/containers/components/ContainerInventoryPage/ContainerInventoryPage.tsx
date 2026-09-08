import Card, { cardType } from "@/shared/components/Card";

export interface ContainerInventoryItem {
  id: string;
  label: string;
  details: string[];
}

interface ContainerInventoryPageProps {
  emptyMessage: string;
  items: ContainerInventoryItem[];
  onSelectItem?: (id: string) => void;
  title: string;
}

const ContainerInventoryPage = ({ emptyMessage, items, onSelectItem, title }: ContainerInventoryPageProps) => (
  <div className="flex flex-col gap-6" data-testid="container-inventory-page">
    <h1 className="text-heading-300">{title}</h1>
    {items.length ? (
      <div className="grid grid-cols-1 gap-4 tablet:grid-cols-2 desktop:grid-cols-3">
        {items.map((item) => (
          <div key={item.id} className="relative">
            {onSelectItem ? (
              <button
                type="button"
                aria-label={`View ${item.label}`}
                className="absolute inset-0 z-10 cursor-pointer rounded-xl border-0 bg-transparent p-0 outline-none focus-visible:ring-2 focus-visible:ring-core-primary-fill focus-visible:ring-offset-2 focus-visible:ring-offset-surface-base"
                onClick={() => onSelectItem(item.id)}
              />
            ) : null}
            <Card
              title={<span className="text-emphasis-300">{item.label}</span>}
              type={cardType.default}
              headerTone="neutral"
              bodyClassName="flex flex-col gap-2 px-4 py-5"
              className={onSelectItem ? "transition-opacity hover:opacity-80" : undefined}
              testId={`container-inventory-${item.id}`}
            >
              {item.details.map((detail) => (
                <p key={detail} className="text-300 text-text-primary-70">
                  {detail}
                </p>
              ))}
            </Card>
          </div>
        ))}
      </div>
    ) : (
      <p className="text-300 text-text-primary-70">{emptyMessage}</p>
    )}
  </div>
);

export default ContainerInventoryPage;
