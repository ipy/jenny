import { Badge } from '../ui-primitives';
import { IconButton } from '../ui-primitives';

// ── BriefingCard ────────────────────────────

export type BriefingStatus = 'generating' | 'ready' | 'failed' | 'dismissed';

export interface BriefingCardProps {
  id: string;
  title: string;
  excerpt?: string;
  status: BriefingStatus;
  timestamp: string;
  isHighlighted?: boolean;
  onRead?: (id: string) => void;
  onDismiss?: (id: string) => void;
  className?: string;
}

/**
 * BriefingCard — Briefing item card
 * Title, status badge, timestamp, optional excerpt, and actions.
 */
export function BriefingCard({
  id,
  title,
  excerpt,
  status,
  timestamp,
  isHighlighted = false,
  onRead,
  onDismiss,
  className = '',
}: BriefingCardProps) {
  const statusConfig: Record<
    BriefingStatus,
    { variant: 'default' | 'primary' | 'accent' | 'danger' | 'success' | 'warning'; label: string }
  > = {
    generating: { variant: 'primary', label: 'Generating' },
    ready: { variant: 'success', label: 'Ready' },
    failed: { variant: 'danger', label: 'Failed' },
    dismissed: { variant: 'default', label: 'Dismissed' },
  };

  const config = statusConfig[status] ?? statusConfig.generating;

  return (
    <article
      className={['glass', isHighlighted ? 'briefing-card-ready' : '', className].filter(Boolean).join(' ')}
      style={{ padding: '1rem' }}
    >
      {/* Header row */}
      <div
        style={{
          display: 'flex',
          alignItems: 'flex-start',
          justifyContent: 'space-between',
          gap: '0.75rem',
          marginBottom: excerpt ? '0.5rem' : 0,
        }}
      >
        <div style={{ flex: 1, minWidth: 0 }}>
          <h3
            style={{
              fontSize: '0.9375rem',
              fontWeight: 700,
              letterSpacing: '-0.01em',
              color: 'var(--color-text)',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
          >
            {title}
          </h3>
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: '0.5rem',
              marginTop: '0.25rem',
            }}
          >
            <Badge variant={config.variant} size="sm" dot={status === 'generating'}>
              {status === 'generating' ? 'Generating…' : config.label}
            </Badge>
            <span
              style={{
                fontSize: '0.6875rem',
                color: 'var(--color-text-dim)',
                fontFamily: 'var(--font-mono)',
              }}
            >
              {timestamp}
            </span>
          </div>
        </div>

        {/* Action buttons */}
        <div
          style={{
            display: 'flex',
            gap: '0.375rem',
            flexShrink: 0,
            alignItems: 'center',
          }}
        >
          {status === 'ready' && onRead && (
            <button
              type="button"
              className="focus-ring"
              onClick={() => onRead(id)}
              style={{
                padding: '0.25rem 0.625rem',
                borderRadius: '8px',
                border: '1px solid oklch(0.55 0.18 285 / 0.3)',
                background: 'oklch(0.55 0.18 285 / 0.1)',
                color: 'var(--color-primary)',
                fontSize: '0.75rem',
                fontWeight: 600,
                cursor: 'pointer',
                transition: 'background 0.15s',
                whiteSpace: 'nowrap',
              }}
              onMouseEnter={(e) => {
                (e.currentTarget as HTMLElement).style.background = 'oklch(0.55 0.18 285 / 0.2)';
              }}
              onMouseLeave={(e) => {
                (e.currentTarget as HTMLElement).style.background = 'oklch(0.55 0.18 285 / 0.1)';
              }}
            >
              Read
            </button>
          )}
          {onDismiss && status !== 'dismissed' && (
            <IconButton
              label="Dismiss"
              size="sm"
              variant="default"
              onClick={() => onDismiss(id)}
            >
              ✕
            </IconButton>
          )}
        </div>
      </div>

      {/* Excerpt */}
      {excerpt && (
        <p
          style={{
            fontSize: '0.8125rem',
            color: 'var(--color-text-muted)',
            lineHeight: 1.6,
            overflow: 'hidden',
            display: '-webkit-box',
            WebkitLineClamp: 3,
            WebkitBoxOrient: 'vertical',
          }}
        >
          {excerpt}
        </p>
      )}
    </article>
  );
}