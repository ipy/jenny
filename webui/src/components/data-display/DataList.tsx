import { type ReactNode } from 'react';

export interface DataListItem<T = string> {
  id: T;
  title: string;
  subtitle?: string;
  badge?: ReactNode;
  showKill?: boolean;
  onKill?: () => void;
  onDelete?: () => void;
}

export interface DataListProps<T = string> {
  items: DataListItem<T>[];
  selectedId?: T | null;
  onSelect: (id: T) => void;
  selectionLabel?: string;
  emptyMessage?: string;
  loading?: boolean;
  footer?: ReactNode;
  className?: string;
}

/**
 * DataList — Selectable list with optional badge and actions
 * Master-detail pattern: session list, task list, etc.
 */
export function DataList<T extends string = string>({
  items,
  selectedId,
  onSelect,
  selectionLabel,
  emptyMessage = 'No items',
  loading = false,
  footer,
  className = '',
}: DataListProps<T>) {
  return (
    <div
      className={className}
      style={{ display: 'flex', flexDirection: 'column', minHeight: 0, flex: 1 }}
    >
      {loading ? (
        <div style={{ padding: '1rem' }}>
          <span style={{ fontSize: '0.875rem', color: 'var(--color-text-muted)' }}>Loading…</span>
        </div>
      ) : items.length === 0 ? (
        <div style={{ padding: '1rem' }}>
          <span style={{ fontSize: '0.875rem', color: 'var(--color-text-dim)' }}>{emptyMessage}</span>
        </div>
      ) : (
        <div style={{ flex: 1, overflowY: 'auto' }}>
          {items.map((item) => (
            <DataListRow
              key={item.id}
              item={item}
              isSelected={item.id === selectedId}
              onSelect={onSelect}
              selectionLabel={selectionLabel}
            />
          ))}
        </div>
      )}

      {footer && (
        <div
          style={{
            borderTop: '1px solid var(--color-border)',
            padding: '0.75rem',
            flexShrink: 0,
          }}
        >
          {footer}
        </div>
      )}
    </div>
  );
}

function DataListRow<T extends string>({
  item,
  isSelected,
  onSelect,
}: {
  item: DataListItem<T>;
  isSelected: boolean;
  onSelect: (id: T) => void;
  selectionLabel?: string;
}) {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'stretch',
        borderBottom: '1px solid var(--color-border)',
        transition: 'background 0.15s',
      }}
    >
      <button
        type="button"
        onClick={() => onSelect(item.id)}
        aria-current={isSelected ? 'true' : undefined}
        className={isSelected ? 'glow-primary data-list-row-selected' : 'focus-ring'}
        style={{
          flex: 1,
          textAlign: 'left',
          padding: '0.75rem',
          minWidth: 0,
          background: isSelected ? 'oklch(0.55 0.18 285 / 0.06)' : 'transparent',
          border: 'none',
          borderLeft: isSelected ? '2px solid var(--color-primary)' : '2px solid transparent',
          cursor: 'pointer',
          transition: 'background 0.15s, border-left-color 0.15s',
        }}
        onMouseEnter={(e) => {
          if (!isSelected) (e.currentTarget as HTMLElement).style.background = 'var(--color-glass-hover)';
        }}
        onMouseLeave={(e) => {
          if (!isSelected) (e.currentTarget as HTMLElement).style.background = 'transparent';
        }}
      >
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            gap: '0.5rem',
            overflow: 'hidden',
          }}
        >
          <span
            style={{
              fontSize: '0.8125rem',
              fontFamily: 'var(--font-mono)',
              color: isSelected ? 'var(--color-text)' : 'var(--color-text-muted)',
              fontWeight: isSelected ? 600 : 400,
              transition: 'color 0.15s',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              flex: 1,
            }}
          >
            {item.title}
          </span>
          {item.badge}
        </div>
        {item.subtitle && (
          <div
            style={{
              fontSize: '0.75rem',
              color: 'var(--color-text-dim)',
              marginTop: '0.25rem',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
          >
            {item.subtitle}
          </div>
        )}
      </button>

      {item.showKill && (
        <ActionButton
          label="Kill"
          onClick={(e) => { e.stopPropagation(); item.onKill?.(); }}
        />
      )}
      {item.onDelete && (
        <ActionButton
          label="Delete"
          onClick={(e) => { e.stopPropagation(); item.onDelete!(); }}
        />
      )}
    </div>
  );
}

function ActionButton({ label, onClick }: { label: string; onClick: (e: React.MouseEvent) => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="focus-ring"
      aria-label={label}
      title={label}
      style={{
        flexShrink: 0,
        padding: '0 0.5rem',
        background: 'none',
        border: 'none',
        borderLeft: '1px solid var(--color-border)',
        color: 'var(--color-danger)',
        cursor: 'pointer',
        fontSize: '11px',
        transition: 'background 0.15s',
      }}
      onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.background = 'oklch(0.55 0.18 25 / 0.1)'; }}
      onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.background = 'transparent'; }}
    >
      {label}
    </button>
  );
}