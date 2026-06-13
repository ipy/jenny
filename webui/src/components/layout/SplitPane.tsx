import { type ReactNode } from 'react';

// ── SplitPane ───────────────────────────────

export interface SplitPaneProps {
  master: ReactNode;
  detail: ReactNode;
  masterWidth?: string;
  stackedOnMobile?: boolean;
  className?: string;
}

/**
 * SplitPane — Master-detail layout
 * Master panel on the left, detail on the right. Stacks vertically on mobile.
 */
export function SplitPane({
  master,
  detail,
  masterWidth = '280px',
  stackedOnMobile = true,
  className = '',
}: SplitPaneProps) {
  return (
    <div
      className={className}
      style={{
        display: 'flex',
        flexDirection: stackedOnMobile ? 'column' : 'row',
        flexWrap: 'nowrap',
        gap: '1rem',
        minHeight: 0,
        alignItems: 'stretch',
        height: '100%',
      }}
    >
      {/* Master panel */}
      <div
        style={{
          width: stackedOnMobile ? '100%' : masterWidth,
          flexShrink: 0,
          borderRadius: '20px',
          overflow: 'hidden',
          display: 'flex',
          flexDirection: 'column',
          minHeight: 0,
          background: 'var(--color-glass)',
          backdropFilter: 'blur(40px) saturate(180%)',
          WebkitBackdropFilter: 'blur(40px) saturate(180%)',
          border: '1px solid var(--color-glass-border)',
          boxShadow: 'var(--shadow-glass)',
        }}
      >
        {master}
      </div>

      {/* Detail panel */}
      <div
        style={{
          flex: 1,
          minWidth: 0,
          borderRadius: '20px',
          overflow: 'hidden',
          display: 'flex',
          flexDirection: 'column',
          minHeight: 0,
          background: 'var(--color-glass)',
          backdropFilter: 'blur(40px) saturate(180%)',
          WebkitBackdropFilter: 'blur(40px) saturate(180%)',
          border: '1px solid var(--color-glass-border)',
          boxShadow: 'var(--shadow-glass)',
        }}
      >
        {detail}
      </div>
    </div>
  );
}