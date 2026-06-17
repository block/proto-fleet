const ResolutionSection = () => {
  // TODO: wire to ticket data — only shown when status is COMPLETED
  return null;
};

export const ResolutionSectionContent = ({
  resolution,
  repairLocation,
  partsUsed,
  notes,
}: {
  resolution: string;
  repairLocation: string;
  partsUsed: Array<{ name: string; quantity: number }>;
  notes: string;
}) => (
  <div className="flex flex-col gap-3">
    <span className="text-emphasis-300 font-medium">Resolution</span>
    <div className="grid grid-cols-2 gap-3">
      <div className="flex flex-col gap-1">
        <span className="text-200 text-text-primary-70">Outcome</span>
        <span className="text-300">{resolution}</span>
      </div>
      <div className="flex flex-col gap-1">
        <span className="text-200 text-text-primary-70">Repair location</span>
        <span className="text-300">{repairLocation}</span>
      </div>
    </div>
    {partsUsed.length > 0 && (
      <div className="flex flex-col gap-1">
        <span className="text-200 text-text-primary-70">Parts used</span>
        {partsUsed.map((part, i) => (
          <span key={i} className="text-300">
            {part.name} x{part.quantity}
          </span>
        ))}
      </div>
    )}
    {notes && (
      <div className="flex flex-col gap-1">
        <span className="text-200 text-text-primary-70">Notes</span>
        <span className="text-300">{notes}</span>
      </div>
    )}
  </div>
);

export default ResolutionSection;
