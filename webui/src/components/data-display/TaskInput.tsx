import { FormField, SelectField, Button } from '../ui-primitives';

// ── TaskInput ───────────────────────────────

export interface TaskInputProps {
  prompt: string;
  onPromptChange: (value: string) => void;
  agentId: string;
  onAgentChange: (value: string) => void;
  agents: string[];
  urgent: boolean;
  onUrgentChange: (value: boolean) => void;
  onSubmit: (e: React.FormEvent) => void;
  isPending?: boolean;
  placeholder?: string;
  className?: string;
}

/**
 * TaskInput — Task creation form
 * Prompt textarea + agent selector + urgent toggle + submit
 *
 * @example
 * <TaskInput
 *   prompt={prompt}
 *   onPromptChange={setPrompt}
 *   agentId={agent}
 *   onAgentChange={setAgent}
 *   agents={['claude', 'gemini']}
 *   urgent={urgent}
 *   onUrgentChange={setUrgent}
 *   onSubmit={handleSubmit}
 * />
 */
export function TaskInput({
  prompt,
  onPromptChange,
  agentId,
  onAgentChange,
  agents,
  urgent,
  onUrgentChange,
  onSubmit,
  isPending = false,
  placeholder = 'Describe the task…',
  className = '',
}: TaskInputProps) {
  return (
    <form
      onSubmit={onSubmit}
      className={['glass-panel glass', className].filter(Boolean).join(' ')}
      style={{ padding: '1.25rem' }}
    >
      {/* Prompt textarea */}
      <div style={{ marginBottom: '1rem' }}>
        <textarea
          value={prompt}
          onChange={(e) => onPromptChange(e.target.value)}
          placeholder={placeholder}
          rows={3}
          className="focus-ring"
          style={{
            width: '100%',
            padding: '0.625rem 0.875rem',
            background: 'var(--color-surface-alt)',
            border: '1px solid var(--color-border)',
            borderRadius: '12px',
            fontSize: '0.875rem',
            lineHeight: 1.6,
            color: 'var(--color-text)',
            resize: 'vertical',
            minHeight: '80px',
            outline: 'none',
            fontFamily: 'inherit',
          }}
        />
      </div>

      {/* Controls row */}
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', flexWrap: 'wrap' }}>
        {/* Agent selector */}
        <div style={{ width: '160px', flexShrink: 0 }}>
          <SelectField
            value={agentId}
            onChange={onAgentChange}
            options={agents.map((a) => ({ value: a, label: a }))}
          />
        </div>

        {/* Urgent toggle */}
        <label
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '0.375rem',
            cursor: 'pointer',
            fontSize: '0.8125rem',
            color: urgent ? 'var(--color-danger)' : 'var(--color-text-muted)',
            fontWeight: urgent ? 600 : 400,
            userSelect: 'none',
          }}
        >
          <input
            type="checkbox"
            checked={urgent}
            onChange={(e) => onUrgentChange(e.target.checked)}
            className="focus-ring"
            style={{ accentColor: 'var(--color-danger)' }}
          />
          Urgent
        </label>

        {/* Submit */}
        <Button
          type="submit"
          variant="primary"
          loading={isPending}
          disabled={!prompt.trim() || isPending}
          style={{ marginLeft: 'auto' }}
        >
          {isPending ? 'Running…' : 'Run Task'}
        </Button>
      </div>
    </form>
  );
}