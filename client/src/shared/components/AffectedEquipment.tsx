import { useState } from "react";
import Button from "@/shared/components/Button";

export default function AffectedEquipment({
  items,
  title = "Affected equipment",
  itemName = "equipment",
}: {
  items: Array<{ id: string; label: string }>;
  title?: string;
  itemName?: string;
}) {
  const [visible, setVisible] = useState(25);
  if (!items.length) return null;
  return (
    <section aria-label={title}>
      <h3 className="font-medium">
        {title} ({items.length})
      </h3>
      <ul className="mt-3 max-h-48 list-inside list-disc overflow-y-auto" data-dd-privacy="mask">
        {items.slice(0, visible).map((item) => (
          <li key={item.id}>{item.label}</li>
        ))}
      </ul>
      {visible < items.length ? (
        <Button variant="textOnly" text={`Show more ${itemName}`} onClick={() => setVisible((count) => count + 25)} />
      ) : null}
    </section>
  );
}
