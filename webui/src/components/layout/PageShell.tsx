import { type ReactNode } from 'react';

// ── PageShell ───────────────────────────────

export interface PageShellProps {
  title?: string;
  badge?: ReactNode;
  actions?: ReactNode;
  busy?: boolean;
  busyLabel?: string;
  children?: ReactNode;
  className?: string;
}

/**
 * PageShell — Main page layout wrapper
 */
export function PageShell({
  title,
  badge,
  actions,
  busy = false,
  busyLabel,
  children,
  className = '',
}: PageShellProps) {
  return (
    <main className={className} style={{ paddingLeft: '2.5rem', paddingRight: '2.5rem', paddingTop: 0 }}>
      {(title || actions) && (
        <header style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '1rem', marginBottom: '0.25rem' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
            {title && (
              <h1 style={{ fontSize: '1.375rem', fontWeight: 800, letterSpacing: '-0.02em', color: 'var(--color-text)' }}>
                {title}
              </h1>
            )}
            {badge}
          </div>
          {actions && (
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
              {busy && (
                <span
                  className="live-indicator"
                  style={{ fontSize: '12px', fontWeight: 500, color: 'var(--color-text-muted)' }}
                  aria-live="polite"
                  aria-label={busyLabel ?? 'Loading'}
                >
                  {busyLabel ?? 'Loading…'}
                </span>
              )}
              {actions}
            </div>
          )}
        </header>
      )}
      <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
        {children}
      </div>
    </main>
  );
}

// ── EmptyState ──────────────────────────────

export interface EmptyStateProps {
  title: string;
  hint?: string;
  icon?: string;
  action?: ReactNode;
}

/**
 * EmptyState — Placeholder for empty data states
 */
export function EmptyState({ title, hint, icon = '○', action }: EmptyStateProps) {
  return (
    <div
      className="glass"
      style={{
        padding: '3rem 2rem',
        textAlign: 'center',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        gap: '0.75rem',
        maxWidth: 'min(480px, 90%)',
      }}
    >
      <span style={{ fontSize: '2rem', opacity: 0.4 }}>{icon}</span>
      <p style={{ fontSize: '0.9375rem', fontWeight: 600, color: 'var(--color-text)' }}>
        {title}
      </p>
      {hint && (
        <p style={{ fontSize: '0.8125rem', color: 'var(--color-text-muted)', maxWidth: 'min(420px, 100%)' }}>
          {hint}
        </p>
      )}
      {action && <div style={{ marginTop: '0.5rem' }}>{action}</div>}
    </div>
  );
}

// ── LoadingState ────────────────────────────

export type LoadingVariant = 'full' | 'inline' | 'skeleton';

export interface LoadingStateProps {
  label?: string;
  variant?: LoadingVariant;
  className?: string;
}

/**
 * LoadingState — Loading indicator
 */
export function LoadingState({
  label = 'Loading…',
  variant = 'inline',
  className = '',
}: LoadingStateProps) {
  if (variant === 'skeleton') {
    return (
      <div className={className} style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
        {[100, 75, 90].map((w, i) => (
          <div
            key={i}
            style={{
              height: '1rem',
              width: `${w}%`,
              background: 'var(--color-glass-subtle)',
              borderRadius: '6px',
              animation: 'pulse 1.5s ease-in-out infinite',
            }}
          />
        ))}
      </div>
    );
  }

  if (variant === 'full') {
    return (
      <div
        className={className}
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          padding: '4rem 2rem',
          flexDirection: 'column',
          gap: '1rem',
        }}
      >
        <span
          className="spinner"
          style={{
            width: '24px',
            height: '24px',
            border: '2px solid var(--color-border)',
            borderTopColor: 'var(--color-primary)',
            borderRadius: '50%',
          }}
        />
        <span style={{ fontSize: '0.875rem', color: 'var(--color-text-muted)' }}>
          {label}
        </span>
      </div>
    );
  }

  return (
    <span
      className={className}
      style={{ fontSize: '0.875rem', color: 'var(--color-text-muted)' }}
      aria-live="polite"
    >
      {label}
    </span>
  );
}

// ── ErrorBanner ─────────────────────────────

export interface ErrorBannerProps {
  message: string;
  onRetry?: () => void;
  className?: string;
}

/**
 * ErrorBanner — Error message display with optional retry
 */
export function ErrorBanner({ message, onRetry, className = '' }: ErrorBannerProps) {
  return (
    <div
      className={className}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: '0.75rem',
        padding: '0.75rem 1rem',
        background: 'oklch(0.55 0.18 25 / 0.1)',
        border: '1px solid oklch(0.55 0.18 25 / 0.2)',
        borderRadius: '10px',
      }}
      role="alert"
    >
      <span
        style={{
          fontSize: '11px',
          fontWeight: 700,
          textTransform: 'uppercase',
          letterSpacing: '0.1em',
          color: 'var(--color-danger)',
          flexShrink: 0,
        }}
      >
        {message}
      </span>
      {onRetry && (
        <button
          type="button"
          className="focus-ring"
          onClick={onRetry}
          style={{
            marginLeft: 'auto',
            background: 'none',
            border: 'none',
            fontSize: '11px',
            color: 'var(--color-text-muted)',
            textDecoration: 'underline',
            cursor: 'pointer',
          }}
        >
          Retry
        </button>
      )}
    </div>
  );
}