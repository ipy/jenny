import type { ColorVariant, BadgeSize } from '../../types';

// ── Badge ───────────────────────────────────

export interface BadgeProps {
  variant?: ColorVariant;
  size?: BadgeSize;
  children: React.ReactNode;
  className?: string;
  dot?: boolean; // show a colored dot before text
}

/**
 * Badge — Status indicator chip
 * Variants: default, primary, accent, danger, success, warning
 *
 * @example
 * <Badge variant="success" dot>Online</Badge>
 */
export function Badge({
  variant = 'default',
  size = 'md',
  children,
  className = '',
  dot = false,
}: BadgeProps) {
  const sizeClasses = {
    sm: 'px-1.5 py-0.5 text-[10px]',
    md: 'px-2 py-0.5 text-[11px]',
  };

  const variantClasses: Record<ColorVariant, string> = {
    default: 'badge-default',
    primary: 'badge-primary',
    accent: 'badge-accent',
    danger: 'badge-danger',
    success: 'badge-success',
    warning: 'badge-warning',
  };

  return (
    <span
      className={['badge', variantClasses[variant], sizeClasses[size], className].filter(Boolean).join(' ')}
      style={{ display: 'inline-flex', alignItems: 'center', gap: '0.25rem' }}
    >
      {dot && (
        <span
          style={{
            width: '6px',
            height: '6px',
            borderRadius: '50%',
            background: 'currentColor',
            flexShrink: 0,
          }}
        />
      )}
      {children}
    </span>
  );
}