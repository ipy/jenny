import { InfoTip } from './InfoTip';

export interface FieldLabelProps {
  children: React.ReactNode;
  htmlFor?: string;
  tooltip?: string;
  optional?: boolean;
  className?: string;
}

/**
 * FieldLabel — Form field label with optional tooltip.
 * Use the `tooltip` prop (not as child) to avoid double tooltip.
 */
export function FieldLabel({
  children,
  htmlFor,
  tooltip,
  optional = false,
  className = '',
}: FieldLabelProps) {
  return (
    <label
      id={htmlFor ? `${htmlFor}-label` : undefined}
      htmlFor={htmlFor}
      className={className}
      style={{ display: 'flex', alignItems: 'center', gap: '0.375rem', fontSize: '0.875rem', fontWeight: 500, color: 'var(--color-text)' }}
    >
      {children}
      {optional && (
        <span style={{ fontSize: '0.75rem', fontWeight: 400, color: 'var(--color-text-dim)' }}>
          (optional)
        </span>
      )}
      {tooltip && <InfoTip text={tooltip} />}
    </label>
  );
}