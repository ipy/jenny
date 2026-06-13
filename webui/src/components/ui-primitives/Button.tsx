import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from 'react';
import type { ColorVariant } from '../../types';

// ── Button ─────────────────────────────────

export type ButtonVariant = ColorVariant | 'ghost' | 'outline' | 'text';
export type ButtonSize = 'xs' | 'sm' | 'md' | 'lg';

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  loading?: boolean;
  icon?: ReactNode;
  iconAfter?: ReactNode;
}

/**
 * Button — Interactive element
 *
 * Variants:
 * - default     glass-subtle surface
 * - primary     solid primary background
 * - accent      solid accent background
 * - danger      solid danger background
 * - success     solid success background
 * - warning     solid warning background
 * - ghost       transparent bg, hover reveals surface-alt
 * - outline     transparent bg, visible border, hover fills subtle
 * - text        no border, hover underline
 *
 * Sizes: xs(24px) / sm(28px) / md(34px) / lg(42px)
 */
export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  {
    variant = 'default',
    size = 'md',
    loading = false,
    icon,
    iconAfter,
    children,
    className = '',
    disabled,
    ...props
  },
  ref
) {
  const sizeStyles: Record<ButtonSize, { padding: string; fontSize: string; height: string }> = {
    xs:   { padding: '0.25rem 0.625rem', fontSize: '11px',  height: '24px' },
    sm:   { padding: '0.375rem 0.75rem', fontSize: '12px',  height: '28px' },
    md:   { padding: '0.5rem 1rem',     fontSize: '13px',  height: '34px' },
    lg:   { padding: '0.625rem 1.25rem', fontSize: '14px', height: '42px' },
  };

  const { padding, fontSize, height } = sizeStyles[size];

  const getBaseStyle = (): React.CSSProperties => {
    switch (variant) {
      case 'primary':  return { background: 'var(--color-primary)',  color: '#fff', border: '1px solid transparent' };
      case 'accent':   return { background: 'var(--color-accent)',   color: '#fff', border: '1px solid transparent' };
      case 'danger':  return { background: 'var(--color-danger)',  color: '#fff', border: '1px solid transparent' };
      case 'success':  return { background: 'var(--color-success)', color: '#fff', border: '1px solid transparent' };
      case 'warning':  return { background: 'var(--color-warning)', color: '#fff', border: '1px solid transparent' };
      case 'ghost':    return { background: 'transparent', color: 'var(--color-text)', border: '1px solid transparent' };
      case 'outline':  return { background: 'transparent', color: 'var(--color-text)', border: '1px solid var(--color-border)' };
      case 'text':     return { background: 'transparent', color: 'var(--color-text)', border: '1px solid transparent' };
      default:         return { background: 'var(--color-glass-subtle)', color: 'var(--color-text)', border: '1px solid var(--color-glass-border)' };
    }
  };

  const baseStyle = getBaseStyle();
  const isDisabled = disabled || loading;

  return (
    <button
      ref={ref}
      className={[
        'btn-hover',
        `btn-variant-${variant}`,
        'focus-ring',
        className,
      ].filter(Boolean).join(' ')}
      style={{
        padding,
        height,
        fontSize,
        fontWeight: 500,
        gap: '0.4em',
        borderRadius: '10px',
        cursor: isDisabled ? 'not-allowed' : 'pointer',
        opacity: isDisabled ? 0.5 : 1,
        transition: 'background 0.15s, border-color 0.15s, color 0.15s, filter 0.15s',
        whiteSpace: 'nowrap',
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        ...baseStyle,
      }}
      disabled={isDisabled}
aria-busy={loading}
onMouseEnter={(e) => {
if (variant === 'ghost') {
(e.currentTarget as HTMLElement).style.borderColor = 'var(--color-border)';
}
('onMouseEnter' in props && (props as any).onMouseEnter?.(e));
}}
onMouseLeave={(e) => {
if (variant === 'ghost') {
(e.currentTarget as HTMLElement).style.borderColor = 'transparent';
}
('onMouseLeave' in props && (props as any).onMouseLeave?.(e));
}}
{...props}
>
{loading ? (
        <span
          style={{
            width: '0.85em',
            height: '0.85em',
            border: '2px solid currentColor',
            borderTopColor: 'transparent',
            borderRadius: '50%',
            animation: 'spin 0.7s linear infinite',
            display: 'inline-block',
            flexShrink: 0,
          }}
        />
      ) : icon ? (
        <span style={{ display: 'inline-flex', alignItems: 'center', fontSize: 'inherit', flexShrink: 0 }}>
          {icon}
        </span>
      ) : null}
      {children}
      {iconAfter && !loading && (
        <span style={{ display: 'inline-flex', alignItems: 'center', fontSize: 'inherit', flexShrink: 0 }}>
          {iconAfter}
        </span>
      )}
    </button>
  );
});