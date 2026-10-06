interface ChangePreviewTableProps {
  label: string;
  subject: string;
  rows: { key: string; label: string; original: string; target: string }[];
}

export default function ChangePreviewTable({ label, subject, rows }: ChangePreviewTableProps) {
  return (
    <table aria-label={label} className="w-full table-fixed text-left text-200">
      <thead className="text-text-primary-70">
        <tr>
          <th scope="col" className="w-2/5 py-2 pr-3 font-normal">
            {subject}
          </th>
          <th scope="col" className="py-2 pr-3 font-normal">
            Original
          </th>
          <th scope="col" className="py-2 font-normal">
            Target
          </th>
        </tr>
      </thead>
      <tbody>
        {rows.map((row) => (
          <tr key={row.key} className="border-t border-border-5 align-top">
            <th scope="row" className="py-2.5 pr-3 font-normal wrap-anywhere">
              {row.label}
            </th>
            <td className="py-2.5 pr-3 wrap-anywhere whitespace-pre-wrap text-text-primary-70">{row.original}</td>
            <td className="py-2.5 wrap-anywhere whitespace-pre-wrap">{row.target}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
