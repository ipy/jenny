import { ErrorBanner } from '../layout';

export interface MutationFeedbackProps {
  mutation: {
    isPending: boolean;
    isSuccess: boolean;
    isError: boolean;
    error?: Error | null;
  };
  successLabel?: string;
  errorDetail?: string;
  errorTechnical?: string;
  onRetry?: () => void;
  className?: string;
}

/**
 * MutationFeedback — Inline success/error feedback for mutations
 */
export function MutationFeedback({
  mutation,
  successLabel = 'Done!',
  errorDetail,
  errorTechnical,
  onRetry,
  className = '',
}: MutationFeedbackProps) {
  if (mutation.isPending) {
    return (
      <span className={className} style={{ fontSize: '11px', fontWeight: 500, color: 'var(--color-text-muted)' }}>
        Saving…
      </span>
    );
  }

  if (mutation.isSuccess) {
    return (
      <span
        className={className}
        style={{
          fontSize: '10px',
          fontWeight: 700,
          textTransform: 'uppercase',
          letterSpacing: '0.1em',
          color: 'var(--color-success)',
        }}
      >
        {successLabel}
      </span>
    );
  }

  if (mutation.isError && errorDetail) {
    return (
      <div className={className}>
        <ErrorBanner message={errorDetail} onRetry={onRetry} />
        {errorTechnical && (
          <p style={{ fontSize: '0.75rem', fontFamily: 'var(--font-mono)', color: 'var(--color-text-dim)', marginTop: '0.25rem' }}>
            {errorTechnical}
          </p>
        )}
      </div>
    );
  }

  return null;
}