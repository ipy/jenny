import { useState, useRef, useEffect, useCallback } from 'react';

// ── InfoTip ─────────────────────────────────

export interface InfoTipProps {
  text: string;
  children?: React.ReactNode; // trigger element, defaults to "ⓘ"
}

/**
 * InfoTip — Inline tooltip with hover/focus trigger
 * Not glass — opaque popover for readability
 *
 * @example
 * <InfoTip text="This is helpful information">Hover me</InfoTip>
 */
export function InfoTip({ text, children }: InfoTipProps) {
  const [visible, setVisible] = useState(false);
  const triggerRef = useRef<HTMLSpanElement>(null);
  const popoverRef = useRef<HTMLSpanElement>(null);
  const showTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const hide = useCallback(() => {
    if (showTimerRef.current) clearTimeout(showTimerRef.current);
    setVisible(false);
  }, []);

  const show = useCallback(() => {
    if (showTimerRef.current) clearTimeout(showTimerRef.current);
    showTimerRef.current = setTimeout(() => setVisible(true), 300);
  }, []);

  useEffect(() => {
    if (!visible) return;

    const handleClickOutside = (e: MouseEvent) => {
      if (
        !triggerRef.current?.contains(e.target as Node) &&
        !popoverRef.current?.contains(e.target as Node)
      ) {
        setVisible(false);
      }
    };

    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setVisible(false);
    };

    document.addEventListener('mousedown', handleClickOutside);
    document.addEventListener('keydown', handleEscape);
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
      document.removeEventListener('keydown', handleEscape);
    };
  }, [visible]);

  // Clean up timer on unmount
  useEffect(() => {
    return () => {
      if (showTimerRef.current) clearTimeout(showTimerRef.current);
    };
  }, []);

  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', position: 'relative', flexShrink: 0 }}>
      <span
        ref={triggerRef}
        className="focus-ring"
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          width: '16px',
          height: '16px',
          borderRadius: '50%',
          background: 'var(--color-glass-subtle)',
          border: '1px solid var(--color-glass-border)',
          fontSize: '9px',
          fontWeight: 700,
          color: 'var(--color-text-muted)',
          cursor: 'help',
          userSelect: 'none',
          flexShrink: 0,
        }}
        onClick={(e) => {
          e.stopPropagation();
          setVisible((v) => !v);
        }}
        onMouseEnter={show}
        onMouseLeave={hide}
        onFocus={() => setVisible(true)}
        onBlur={(e) => {
          if (!triggerRef.current?.contains(e.relatedTarget as Node)) {
            setVisible(false);
          }
        }}
        tabIndex={0}
        aria-label="More information"
        role="button"
      >
        {children ?? 'i'}
      </span>

      {visible && (
        <span
          ref={popoverRef}
          className="info-tip-popover animate-fade-in"
          style={{
            position: 'absolute',
            bottom: 'calc(100% + 6px)',
            left: '50%',
            transform: 'translateX(-50%)',
            padding: '0.5rem 0.75rem',
            borderRadius: '8px',
            fontSize: '12px',
            lineHeight: 1.5,
            maxWidth: '260px',
            width: 'max-content',
            zIndex: 50,
            pointerEvents: 'none',
          }}
          role="tooltip"
        >
          {text}
        </span>
      )}
    </span>
  );
}