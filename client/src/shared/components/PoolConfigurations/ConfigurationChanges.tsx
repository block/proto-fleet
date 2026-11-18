export type ConfigurationChange = { field: string; before: string; after: string };

export const ConfigurationChanges = ({ changes }: { changes: ConfigurationChange[] }) => (
  <dl className="flex flex-col gap-3" data-dd-privacy="mask">
    {changes.map((change) => (
      <div key={change.field}>
        <dt className="font-medium">{change.field}</dt>
        <dd className="break-all">
          <span className="text-text-primary-50">{change.before || "None"}</span> → {change.after || "None"}
        </dd>
      </div>
    ))}
  </dl>
);
