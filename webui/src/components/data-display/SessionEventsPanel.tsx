import { useState } from 'react';
import { Badge } from '../ui-primitives';
import type { SessionEvent, SessionEventKind } from '../../types';

// ── SessionEventsPanel ──────────────────────

export interface SessionEventsPanelProps {
  sessionId: string;
  events: SessionEvent[];
  isRunning: boolean;
  messageCount?: number;
  className?: string;
}

// ── Event kind → badge variant mapping ──────
const KIND_CONFIG: Record<
  SessionEventKind,
  { variant: 'default' | 'primary' | 'accent' | 'danger' | 'success' | 'warning'; label: string }
> = {
  init: { variant: 'default', label: 'INIT' },
  user: { variant: 'primary', label: 'USER' },
  assistant: { variant: 'accent', label: 'AI' },
  tool: { variant: 'warning', label: 'TOOL' },
  tool_result: { variant: 'default', label: 'RES' },
  thinking: { variant: 'default', label: 'THK' },
  result: { variant: 'success', label: 'DONE' },
  other: { variant: 'default', label: 'NOTE' },
};

/**
 * SessionEventsPanel — Collapsible timeline of session events
 *
 * @example
 * <SessionEventsPanel
 *   sessionId="sess-123"
 *   events={events}
 *   isRunning={isRunning}
 * />
 */
export function SessionEventsPanel({
  sessionId,
  events,
  isRunning,
  messageCount,
  className = '',
}: SessionEventsPanelProps) {
  const [expanded, setExpanded] = useState(false);

  return (
    <div className={['glass-subtle', className].filter(Boolean).join(' ')} style={{ borderRadius: '14px' }}>
      {/* Header */}
      <button
        type="button"
        className="focus-ring"
        style={{
          width: '100%',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '0.75rem 1rem',
          background: 'none',
          border: 'none',
          cursor: 'pointer',
          gap: '0.5rem',
        }}
        onClick={() => setExpanded((e) => !e)}
        aria-expanded={expanded}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <span style={{ fontSize: '0.75rem', fontWeight: 700, color: 'var(--color-text-muted)' }}>
            {expanded ? '▼' : '▶'}
          </span>
          <span style={{ fontSize: '0.8125rem', fontWeight: 600, color: 'var(--color-text)' }}>
            Activity
          </span>
          {messageCount !== undefined && (
            <Badge variant="default" size="sm">
              {messageCount} msg{messageCount !== 1 ? 's' : ''}
            </Badge>
          )}
          {isRunning && (
            <span className="live-indicator" style={{ fontSize: '10px', color: 'var(--color-primary)' }}>
              ●
            </span>
          )}
        </div>
      </button>

      {/* Timeline */}
      {expanded && (
        <div
          style={{
            borderTop: '1px solid var(--color-border)',
            maxHeight: '300px',
            overflowY: 'auto',
          }}
        >
          {events.length === 0 ? (
            <div
              style={{
                padding: '1rem',
                textAlign: 'center',
                fontSize: '0.8125rem',
                color: 'var(--color-text-dim)',
              }}
            >
              No events yet
            </div>
          ) : (
            events.map((event, idx) => {
              const config = KIND_CONFIG[event.kind] ?? KIND_CONFIG.other;
              return (
                <div
                  key={event.id}
                  style={{
                    display: 'flex',
                    gap: '0.625rem',
                    padding: '0.5rem 1rem',
                    borderBottom:
                      idx < events.length - 1 ? '1px solid var(--color-border)' : 'none',
                    alignItems: 'flex-start',
                  }}
                >
                  <Badge variant={config.variant} size="sm">
                    {config.label}
                  </Badge>

                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div
                      style={{
                        fontSize: '0.8125rem',
                        color: 'var(--color-text)',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                      }}
                    >
                      {event.preview}
                    </div>

                    {event.timestamp_ms && (
                      <div
                        style={{
                          fontSize: '0.6875rem',
                          color: 'var(--color-text-dim)',
                          marginTop: '0.125rem',
                          fontFamily: 'var(--font-mono)',
                        }}
                      >
                        {new Date(event.timestamp_ms).toLocaleTimeString()}
                      </div>
                    )}

                    {event.full && (
                      <pre
                        style={{
                          fontSize: '0.75rem',
                          color: 'var(--color-text-muted)',
                          marginTop: '0.375rem',
                          padding: '0.5rem',
                          background: 'var(--color-glass-subtle)',
                          borderRadius: '6px',
                          overflow: 'auto',
                          maxHeight: '120px',
                          whiteSpace: 'pre-wrap',
                          fontFamily: 'var(--font-mono)',
                          lineHeight: 1.5,
                        }}
                      >
                        {event.full}
                      </pre>
                    )}
                  </div>

                  {event.hasResult && (
                    <span
                      style={{
                        fontSize: '0.625rem',
                        color: 'var(--color-success)',
                        fontWeight: 700,
                        flexShrink: 0,
                      }}
                    >
                      ✓
                    </span>
                  )}
                </div>
              );
            })
          )}
        </div>
      )}
    </div>
  );
}