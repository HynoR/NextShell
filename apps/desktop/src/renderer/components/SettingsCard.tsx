export const SettingsCard = ({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: React.ReactNode;
}) => (
  <div className="stg-card">
    <div className="stg-card-header">
      <div className="stg-card-title">{title}</div>
      {description && <div className="stg-card-desc">{description}</div>}
    </div>
    <div className="stg-card-body">{children}</div>
  </div>
);
