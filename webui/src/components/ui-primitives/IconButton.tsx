import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from 'react';

// ── IconButton ──────────────────────────────

export interface IconButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  label: string; // accessible label (required for icon-only buttons)
  size?: 'sm' | 'md' | 'lg';
  variant?: 'default' | 'primary' | 'danger';
  children: ReactNode;
  buttonRef?: React.Ref<HTMLButtonElement>;
}

/**
 * IconButton — Square icon-only button
 * Use for toolbar actions, close buttons, etc.
 *
 * @example
 * <IconButton label="Close panel" onClick={handleClose} size="md">
 *   ✕
 * </IconButton>
 */
export const IconButton = forwardRef<HTMLButtonElement, IconButtonProps>(function IconButton(
  {
    label,
    size = 'md',
    variant = 'default',
    children,
    className = '',
    disabled,
    buttonRef,
    ...props
  },
  ref
) {
  const sizeMap = {
    sm: '28px',
    md: '32px',
    lg: '40px',
  };

  const variantMap = {
    default: 'glass-subtle',
    primary: 'bg-[var(--color-primary)] text-white',
    danger: 'bg-[var(--color-danger)] text-white',
  };

  return (
    <button
      ref={buttonRef ?? ref}
      type="button"
      className={[
        'focus-ring inline-flex items-center justify-center rounded-[10px] transition-all flex-shrink-0',
        variantMap[variant],
        disabled ? 'opacity-50 cursor-not-allowed' : '',
        className,
      ].filter(Boolean).join(' ')}
      style={{
        width: sizeMap[size],
        height: sizeMap[size],
        fontSize: size === 'sm' ? '12px' : size === 'md' ? '14px' : '16px',
      }}
      disabled={disabled}
      aria-label={label}
      title={label}
      {...props}
    >
      {children}
    </button>
  );
});