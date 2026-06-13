import { useState } from 'react';
import { TextField, SelectField, Button, Badge } from '../ui-primitives';

export interface CronEntryData {
  id: string;
  name: string;
  expression: string;
  pipeline: string;
  agent?: string;
  enabled?: boolean;
  last_run_at?: string;
  next_run_at?: string;
}

export interface CronEditorProps {
  entries: CronEntryData[];
  pipelines: { id: string; label: string }[];
  agents: string[];
  onSave: (entry: CronEntryData) => void;
  onDelete: (id: string) => void;
  onToggle: (id: string, enabled: boolean) => void;
  onRunNow?: (id: string) => void;
  className?: string;
}

/**
 * CronEditor — Cron schedule management UI
 */
export function CronEditor({
  entries,
  pipelines,
  agents,
  onSave,
  onDelete,
  onToggle,
  onRunNow,
  className = '',
}: CronEditorProps) {
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editName, setEditName] = useState('');
  const [editExpr, setEditExpr] = useState('');
  const [editPipeline, setEditPipeline] = useState('');
  const [editAgent, setEditAgent] = useState('');
  const [newName, setNewName] = useState('');
  const [newExpr, setNewExpr] = useState('');
  const [newPipeline, setNewPipeline] = useState(pipelines[0]?.id ?? '');
  const [newAgent, setNewAgent] = useState(agents[0] ?? '');
  const [showNewForm, setShowNewForm] = useState(false);

  const startEdit = (entry: CronEntryData) => {
    setEditingId(entry.id);
    setEditName(entry.name);
    setEditExpr(entry.expression);
    setEditPipeline(entry.pipeline);
    setEditAgent(entry.agent ?? agents[0] ?? '');
  };

  const cancelEdit = () => {
    setEditingId(null);
    setEditName('');
    setEditExpr('');
    setEditPipeline('');
    setEditAgent('');
  };

  const saveEdit = () => {
    if (!editingId || !editName.trim() || !editExpr.trim() || !editPipeline) return;
    onSave({
      id: editingId,
      name: editName.trim(),
      expression: editExpr.trim(),
      pipeline: editPipeline,
      agent: editAgent || undefined,
      enabled: entries.find((e) => e.id === editingId)?.enabled,
    });
    cancelEdit();
  };

  const saveNew = () => {
    if (!newName.trim() || !newExpr.trim() || !newPipeline) return;
    onSave({
      id: `new-${Date.now()}`,
      name: newName.trim(),
      expression: newExpr.trim(),
      pipeline: newPipeline,
      agent: newAgent || undefined,
      enabled: true,
    });
    setNewName('');
    setNewExpr('');
    setNewPipeline(pipelines[0]?.id ?? '');
    setNewAgent(agents[0] ?? '');
    setShowNewForm(false);
  };

  return (
    <div className={className} style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
      {entries.map((entry) => (
        <div key={entry.id} style={{ padding: '0.875rem 1rem', borderRadius: '14px', background: 'var(--color-glass-subtle)', border: '1px solid var(--color-glass-border)' }}>
          {editingId === entry.id ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.5rem' }}>
                <TextField value={editName} onChange={setEditName} placeholder="Job name" />
                <TextField value={editExpr} onChange={setEditExpr} placeholder="*/5 * * * *" />
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.5rem' }}>
                <SelectField
                  value={editPipeline}
                  onChange={setEditPipeline}
                  options={pipelines.map((p) => ({ value: p.id, label: p.label }))}
                />
                <SelectField
                  value={editAgent}
                  onChange={setEditAgent}
                  options={agents.map((a) => ({ value: a, label: a }))}
                />
              </div>
              <div style={{ display: 'flex', gap: '0.5rem', justifyContent: 'flex-end' }}>
                <Button size="sm" variant="default" onClick={cancelEdit}>Cancel</Button>
                <Button size="sm" variant="primary" onClick={saveEdit}>Save</Button>
              </div>
            </div>
          ) : (
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
              <ToggleSwitch
                checked={entry.enabled !== false}
                onChange={() => onToggle(entry.id, !entry.enabled)}
              />
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                  <span style={{ fontWeight: 600, fontSize: '0.875rem' }}>{entry.name}</span>
                  <Badge variant="default" size="sm">{entry.pipeline}</Badge>
                  {entry.agent && <Badge variant="default" size="sm">{entry.agent}</Badge>}
                </div>
                <div style={{ fontSize: '0.75rem', fontFamily: 'var(--font-mono)', color: 'var(--color-text-dim)', marginTop: '0.125rem' }}>
                  {entry.expression}
                </div>
              </div>
              {entry.next_run_at && (
                <span style={{ fontSize: '0.6875rem', color: 'var(--color-text-dim)', flexShrink: 0 }}>
                  next: {new Date(entry.next_run_at).toLocaleString()}
                </span>
              )}
              <div style={{ display: 'flex', gap: '0.25rem', flexShrink: 0 }}>
                {onRunNow && <Button size="sm" variant="default" onClick={() => onRunNow(entry.id)}>Run</Button>}
                <Button size="sm" variant="default" onClick={() => startEdit(entry)}>Edit</Button>
                <Button size="sm" variant="danger" onClick={() => onDelete(entry.id)}>✕</Button>
              </div>
            </div>
          )}
        </div>
      ))}

      {showNewForm ? (
        <div style={{ padding: '0.875rem 1rem', borderRadius: '14px', background: 'var(--color-glass-subtle)', border: '1px solid var(--color-glass-border)' }}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.5rem' }}>
              <TextField value={newName} onChange={setNewName} placeholder="Job name" />
              <TextField value={newExpr} onChange={setNewExpr} placeholder="*/5 * * * *" />
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.5rem' }}>
              <SelectField
                value={newPipeline}
                onChange={setNewPipeline}
                options={pipelines.map((p) => ({ value: p.id, label: p.label }))}
              />
              <SelectField
                value={newAgent}
                onChange={setNewAgent}
                options={agents.map((a) => ({ value: a, label: a }))}
              />
            </div>
            <div style={{ display: 'flex', gap: '0.5rem', justifyContent: 'flex-end' }}>
              <Button size="sm" variant="default" onClick={() => setShowNewForm(false)}>Cancel</Button>
              <Button size="sm" variant="primary" onClick={saveNew}>Add</Button>
            </div>
          </div>
        </div>
      ) : (
        <Button variant="default" onClick={() => setShowNewForm(true)}>+ Add Schedule</Button>
      )}
    </div>
  );
}

function ToggleSwitch({ checked, onChange }: { checked: boolean; onChange: () => void }) {
  return (
    <button
      type="button"
      onClick={onChange}
      className="focus-ring"
      style={{
        width: '36px',
        height: '20px',
        borderRadius: '10px',
        background: checked ? 'var(--color-success)' : 'var(--color-border)',
        border: 'none',
        cursor: 'pointer',
        position: 'relative',
        transition: 'background 0.2s',
        flexShrink: 0,
      }}
      aria-checked={checked}
      role="switch"
    >
      <span
        style={{
          position: 'absolute',
          top: '2px',
          left: checked ? '18px' : '2px',
          width: '16px',
          height: '16px',
          borderRadius: '50%',
          background: 'white',
          transition: 'left 0.2s',
        }}
      />
    </button>
  );
}