import { useState, type ReactNode } from 'react';
import type { CollapsibleVariant } from '../../types';

// ── CollapsibleContentBlock ─────────────────

export interface CollapsibleContentBlockProps {
  title: ReactNode;
  meta?: ReactNode;
  variant?: CollapsibleVariant;
  children: ReactNode;
  collapsedClass?: string;
  expandedClass?: string;
  defaultCollapsed?: boolean;
  className?: string;
}

/**
 * CollapsibleContentBlock — Expandable/collapsible section
 * Variants: default (purple), accent (green), danger (red), muted (gray)
 *
 * @example
 * <CollapsibleContentBlock title="Advanced Settings" variant="default">
 *   <p>Advanced configuration options...</p>
 * </CollapsibleContentBlock>
 */
export function CollapsibleContentBlock({
  title,
  meta,
  variant = 'default',
  children,
  collapsedClass = '',
  expandedClass = '',
  defaultCollapsed = false,
  className = '',
}: CollapsibleContentBlockProps) {
  const [collapsed, setCollapsed] = useState(defaultCollapsed);

  const variantAccentColors: Record<CollapsibleVariant, string> = {
    default: 'var(--color-primary)',
    accent: 'var(--color-accent)',
    danger: 'var(--color-danger)',
    muted: 'var(--color-text-dim)',
  };

  const accentColor = variantAccentColors[variant];

  return (
    <div
      className={['glass-subtle', className].filter(Boolean).join(' ')}
      style={{
        borderRadius: '14px',
        overflow: 'hidden',
        transition: 'background 0.25s cubic-bezier(0.16, 1, 0.3, 1)',
      }}
    >
      {/* Header */}
      <button
        type="button"
        className="focus-ring"
        style={{
          width: '100%',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '0.875rem 1rem',
          background: 'none',
          border: 'none',
          cursor: 'pointer',
          gap: '0.75rem',
        }}
        onClick={() => setCollapsed((c) => !c)}
        aria-expanded={!collapsed}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', minWidth: 0 }}>
          <span
            style={{
              fontSize: '0.8125rem',
              fontWeight: 700,
              color: accentColor,
              flexShrink: 0,
            }}
          >
            {collapsed ? '▶' : '▼'}
          </span>
          <span
            style={{
              fontSize: '0.875rem',
              fontWeight: 600,
              color: 'var(--color-text)',
              textAlign: 'left',
            }}
          >
            {title}
          </span>
          {meta && (
            <span style={{ color: 'var(--color-text-muted)', fontSize: '0.75rem' }}>
              {meta}
            </span>
          )}
        </div>
      </button>

      {/* Content */}
      <div
        style={{
          overflow: 'hidden',
          transition: 'max-height 0.3s cubic-bezier(0.16, 1, 0.3, 1)',
          maxHeight: collapsed ? '0' : '2000px',
        }}
      >
        <div
          style={{
            // collapsed: only top/bottom padding to hide content; expanded: add top gap after divider
            padding: collapsed ? '0 1rem' : '0.875rem 1rem 1rem',
            borderTop: `1px solid var(--color-border)`,
          }}
        >
          <div className={[collapsedClass, expandedClass].filter(Boolean).join(' ')}>
            {children}
          </div>
        </div>
      </div>
    </div>
  );
}