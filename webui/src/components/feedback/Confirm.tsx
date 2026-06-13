import { useEffect, useCallback, useState, createContext, useContext, type ReactNode } from 'react';
import { Portal } from './Portal';
import type { ConfirmOptions } from '../../types';

// ── Confirm Context ─────────────────────────

interface ConfirmContextValue {
  confirm: (options: ConfirmOptions) => Promise<boolean>;
}

const ConfirmCtx = createContext<ConfirmContextValue | null>(null);

// ── ConfirmProvider ─────────────────────────

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [dialog, setDialog] = useState<ConfirmOptions | null>(null);
  const [resolveRef, setResolveRef] = useState<((value: boolean) => void) | null>(null);

  const confirm = useCallback((options: ConfirmOptions): Promise<boolean> => {
    return new Promise((resolve) => {
      setResolveRef(() => resolve);
      setDialog(options);
    });
  }, []);

  const handleConfirm = useCallback(() => {
    resolveRef?.(true);
    setDialog(null);
    setResolveRef(null);
  }, [resolveRef]);

  const handleCancel = useCallback(() => {
    resolveRef?.(false);
    setDialog(null);
    setResolveRef(null);
  }, [resolveRef]);

  // Escape key
  useEffect(() => {
    if (!dialog) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') handleCancel();
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [dialog, handleCancel]);

  return (
    <ConfirmCtx.Provider value={{ confirm }}>
      {children}
      {dialog && (
        <Portal>
          <div
            role="alertdialog"
            aria-modal="true"
            aria-labelledby="confirm-title"
            style={{
              position: 'fixed',
              inset: 0,
              zIndex: 9999,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
            }}
          >
            {/* Backdrop */}
            <div
              style={{
                position: 'absolute',
                inset: 0,
                background: 'oklch(0 0 0 / 0.45)',
                backdropFilter: 'blur(4px)',
              }}
              onClick={handleCancel}
            />

            {/* Dialog */}
            <div
              className="glass"
              style={{
                position: 'relative',
                zIndex: 1,
                width: '100%',
                maxWidth: '420px',
                margin: '0 1rem',
                padding: '1.75rem',
                borderRadius: '20px',
              }}
            >
              <h2
                id="confirm-title"
                style={{
                  fontSize: '1.0625rem',
                  fontWeight: 800,
                  letterSpacing: '-0.02em',
                  marginBottom: '0.5rem',
                }}
              >
                {dialog.title}
              </h2>

              {dialog.message && (
                <p
                  style={{
                    fontSize: '0.875rem',
                    color: 'var(--color-text-muted)',
                    lineHeight: 1.6,
                  }}
                >
                  {dialog.message}
                </p>
              )}

              <div
                style={{
                  display: 'flex',
                  gap: '0.75rem',
                  marginTop: '1.5rem',
                  justifyContent: 'flex-end',
                }}
              >
                <button
                  type="button"
                  className="glass-subtle focus-ring"
                  style={{
                    padding: '0.5rem 1rem',
                    border: 'none',
                    borderRadius: '10px',
                    fontSize: '0.875rem',
                    fontWeight: 500,
                    cursor: 'pointer',
                  }}
                  onClick={handleCancel}
                >
                  {dialog.cancelLabel ?? 'Cancel'}
                </button>
                <button
                  type="button"
                  className="focus-ring"
                  style={{
                    padding: '0.5rem 1rem',
                    border: 'none',
                    borderRadius: '10px',
                    fontSize: '0.875rem',
                    fontWeight: 600,
                    cursor: 'pointer',
                    background: dialog.dangerous
                      ? 'var(--color-danger)'
                      : 'var(--color-primary)',
                    color: 'white',
                  }}
                  onClick={handleConfirm}
                >
                  {dialog.confirmLabel ?? 'Confirm'}
                </button>
              </div>
            </div>
          </div>
        </Portal>
      )}
    </ConfirmCtx.Provider>
  );
}

/**
 * useConfirm — Promise-based confirmation dialog
 *
 * @example
 * const { confirm } = useConfirm();
 * const ok = await confirm({ title: 'Delete item?', message: 'This cannot be undone', dangerous: true });
 * if (ok) deleteItem();
 */
export function useConfirm() {
  const ctx = useContext(ConfirmCtx);
  if (!ctx) throw new Error('useConfirm must be used within ConfirmProvider');
  return ctx;
}