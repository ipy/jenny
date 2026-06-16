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
  masterWidth = '320px',
  stackedOnMobile = false,
  className = '',
}: SplitPaneProps) {
  return (
    <div
      className={['split-pane', className].filter(Boolean).join(' ')}
      style={{
        '--split-master-width': masterWidth,
        display: 'flex',
        flexDirection: 'row',
        flexWrap: 'nowrap',
        gap: '0.75rem',
        minHeight: 0,
        alignItems: 'stretch',
        height: '100%',
      } as React.CSSProperties}
    >
      <div className="split-pane-master" aria-label="Master panel">
        {master}
      </div>
      <div className="split-pane-detail" aria-label="Detail panel">
        {detail}
      </div>
    </div>
  );
}