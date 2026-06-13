import { InfoTip } from '../ui-primitives';

// ── StatCard ────────────────────────────────

export interface StatCardProps {
  label: string;
  value: React.ReactNode;
  tooltip?: string;
  className?: string;
}

/**
 * StatCard — Dashboard stat display with optional tooltip
 */
export function StatCard({ label, value, tooltip, className = '' }: StatCardProps) {
  return (
    <div
      className={className}
      style={{
        padding: '0.75rem',
        borderRadius: '14px',
        background: 'var(--color-glass-subtle)',
        border: '1px solid var(--color-glass-border)',
        minWidth: 0,
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '0.25rem',
          marginBottom: '0.25rem',
        }}
      >
        <span
          style={{
            fontSize: '11px',
            fontWeight: 700,
            textTransform: 'uppercase',
            letterSpacing: '0.1em',
            color: 'var(--color-text-muted)',
          }}
        >
          {label}
        </span>
        {tooltip && <InfoTip text={tooltip} />}
      </div>
      <div
        style={{
          fontSize: '1.25rem',
          fontWeight: 800,
          letterSpacing: '-0.02em',
          color: 'var(--color-text)',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
        }}
      >
        {value}
      </div>
    </div>
  );
}