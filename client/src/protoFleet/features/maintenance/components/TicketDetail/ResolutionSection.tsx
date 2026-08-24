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
    <table aria-label="Ticket resolution" className="w-full border-collapse">
      <tbody className="border-y border-border-5">
        <tr className="border-b border-border-5">
          <th scope="row" className="w-2/5 py-3 pr-3 text-left text-300 font-normal text-text-primary-70">
            Outcome
          </th>
          <td className="py-3 pl-3 text-right text-300 text-text-primary">{resolution}</td>
        </tr>
        <tr className={partsUsed.length > 0 || notes ? "border-b border-border-5" : undefined}>
          <th scope="row" className="w-2/5 py-3 pr-3 text-left text-300 font-normal text-text-primary-70">
            Repair location
          </th>
          <td className="py-3 pl-3 text-right text-300 text-text-primary">{repairLocation}</td>
        </tr>
        {partsUsed.length > 0 ? (
          <tr className={notes ? "border-b border-border-5" : undefined}>
            <th scope="row" className="w-2/5 py-3 pr-3 text-left text-300 font-normal text-text-primary-70">
              Parts used
            </th>
            <td className="py-3 pl-3 text-right text-300 text-text-primary">
              {partsUsed.map((part) => (
                <div key={part.name}>
                  {part.name} ×{part.quantity}
                </div>
              ))}
            </td>
          </tr>
        ) : null}
        {notes ? (
          <tr>
            <th scope="row" className="w-2/5 py-3 pr-3 text-left text-300 font-normal text-text-primary-70">
              Notes
            </th>
            <td className="py-3 pl-3 text-right text-300 whitespace-pre-wrap text-text-primary">{notes}</td>
          </tr>
        ) : null}
      </tbody>
    </table>
  </div>
);

export default ResolutionSection;
