import { createContext, useContext, useState, useCallback, useEffect, type ReactNode } from 'react';
import { Portal } from './Portal';
import type { ToastMessage } from '../../types';

// ── Toast Context ───────────────────────────

interface ToastContextValue {
  addToast: (toast: Omit<ToastMessage, 'id'>) => string;
  removeToast: (id: string) => void;
}

const ToastCtx = createContext<ToastContextValue | null>(null);

// ── ToastProvider ───────────────────────────

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastMessage[]>([]);

  const addToast = useCallback((toast: Omit<ToastMessage, 'id'>): string => {
    const id = `toast-${Date.now()}-${Math.random().toString(36).slice(2)}`;
    setToasts((prev) => [...prev, { ...toast, id }]);

    if (toast.duration !== 0) {
      setTimeout(() => {
        setToasts((prev) => prev.filter((t) => t.id !== id));
      }, toast.duration ?? 4000);
    }

    return id;
  }, []);

  const removeToast = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  return (
    <ToastCtx.Provider value={{ addToast, removeToast }}>
      {children}
      <Portal>
        <div
          aria-live="polite"
          aria-label="Notifications"
          style={{
            position: 'fixed',
            bottom: '1.5rem',
            right: '1.5rem',
            zIndex: 9999,
            display: 'flex',
            flexDirection: 'column',
            gap: '0.5rem',
            pointerEvents: 'none',
            maxWidth: '400px',
          }}
        >
          {toasts.map((toast) => (
            <ToastItem key={toast.id} toast={toast} onDismiss={() => removeToast(toast.id)} />
          ))}
        </div>
      </Portal>
    </ToastCtx.Provider>
  );
}

// ── ToastItem ───────────────────────────────

const VARIANT_STYLES: Record<ToastMessage['kind'], { bg: string; color: string; icon: string }> = {
  info: { bg: 'oklch(0.55 0.18 285 / 0.15)', color: 'var(--color-primary)', icon: 'ℹ' },
  success: { bg: 'oklch(0.6 0.15 150 / 0.15)', color: 'var(--color-success)', icon: '✓' },
  warning: { bg: 'oklch(0.65 0.15 50 / 0.15)', color: 'var(--color-warning)', icon: '⚠' },
  error: { bg: 'oklch(0.55 0.18 25 / 0.15)', color: 'var(--color-danger)', icon: '✕' },
};

function ToastItem({ toast, onDismiss }: { toast: ToastMessage; onDismiss: () => void }) {
  const style = VARIANT_STYLES[toast.kind];

  return (
    <div
      className="glass animate-fade-in"
      style={{
        padding: '0.75rem 1rem',
        minWidth: '280px',
        display: 'flex',
        alignItems: 'flex-start',
        gap: '0.75rem',
        pointerEvents: 'auto',
        borderLeft: `3px solid ${style.color}`,
      }}
      role="alert"
    >
      <span
        style={{
          width: '20px',
          height: '20px',
          borderRadius: '50%',
          background: style.bg,
          color: style.color,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          fontSize: '10px',
          fontWeight: 700,
          flexShrink: 0,
        }}
      >
        {style.icon}
      </span>

      <div style={{ flex: 1, minWidth: 0 }}>
        <div
          style={{
            fontSize: '0.875rem',
            fontWeight: 600,
            color: 'var(--color-text)',
          }}
        >
          {toast.title}
        </div>
        {toast.message && (
          <div
            style={{
              fontSize: '0.8125rem',
              color: 'var(--color-text-muted)',
              marginTop: '0.25rem',
              lineHeight: 1.5,
            }}
          >
            {toast.message}
          </div>
        )}
      </div>

      <button
        type="button"
        className="focus-ring"
        style={{
          background: 'none',
          border: 'none',
          padding: '0.125rem',
          color: 'var(--color-text-dim)',
          cursor: 'pointer',
          fontSize: '12px',
          flexShrink: 0,
        }}
        onClick={onDismiss}
        aria-label="Dismiss"
      >
        ✕
      </button>
    </div>
  );
}

/**
 * useToast — Add toast notifications
 *
 * @example
 * const toast = useToast();
 * toast.addToast({ kind: 'success', title: 'Saved!', message: 'Your changes were saved.' });
 * toast.addToast({ kind: 'error', title: 'Failed', message: 'Network error', duration: 0 }); // persistent
 */
export function useToast() {
  const ctx = useContext(ToastCtx);
  if (!ctx) throw new Error('useToast must be used within ToastProvider');
  return ctx;
}