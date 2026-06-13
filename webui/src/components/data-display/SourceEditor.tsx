import { useState } from 'react';
import { TextField, SelectField, Button, Badge } from '../ui-primitives';

export type SourceType = 'rss' | 'scraper';

export interface SourceEntryData {
  id: string;
  type: SourceType;
  name: string;
  url: string;
  enabled?: boolean;
  last_fetched_at?: string;
  agent?: string;
}

export interface SourceEditorProps {
  sources: SourceEntryData[];
  agents: string[];
  onSave: (source: SourceEntryData) => void;
  onDelete: (id: string) => void;
  onToggle: (id: string, enabled: boolean) => void;
  onTest?: (id: string) => void;
  className?: string;
}

/**
 * SourceEditor — Feed source management UI
 */
export function SourceEditor({
  sources,
  agents,
  onSave,
  onDelete,
  onToggle,
  onTest,
  className = '',
}: SourceEditorProps) {
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editName, setEditName] = useState('');
  const [editUrl, setEditUrl] = useState('');
  const [editAgent, setEditAgent] = useState('');
  const [editType, setEditType] = useState<SourceType>('rss');
  const [newName, setNewName] = useState('');
  const [newUrl, setNewUrl] = useState('');
  const [newAgent, setNewAgent] = useState(agents[0] ?? '');
  const [newType, setNewType] = useState<SourceType>('rss');
  const [showNewForm, setShowNewForm] = useState(false);

  const startEdit = (source: SourceEntryData) => {
    setEditingId(source.id);
    setEditName(source.name);
    setEditUrl(source.url);
    setEditAgent(source.agent ?? agents[0] ?? '');
    setEditType(source.type);
  };

  const cancelEdit = () => {
    setEditingId(null);
    setEditName('');
    setEditUrl('');
    setEditAgent('');
  };

  const saveEdit = () => {
    if (!editingId || !editName.trim() || !editUrl.trim()) return;
    onSave({ id: editingId, name: editName.trim(), url: editUrl.trim(), agent: editAgent || undefined, type: editType });
    cancelEdit();
  };

  const saveNew = () => {
    if (!newName.trim() || !newUrl.trim()) return;
    onSave({ id: `new-${Date.now()}`, name: newName.trim(), url: newUrl.trim(), agent: newAgent || undefined, type: newType });
    setNewName('');
    setNewUrl('');
    setNewAgent(agents[0] ?? '');
    setShowNewForm(false);
  };

  return (
    <div className={className} style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
      {sources.map((source) => (
        <div key={source.id} style={{ padding: '0.875rem 1rem', borderRadius: '14px', background: 'var(--color-glass-subtle)', border: '1px solid var(--color-glass-border)' }}>
          {editingId === source.id ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
              <TextField value={editName} onChange={setEditName} placeholder="Source name" />
              <TextField value={editUrl} onChange={setEditUrl} placeholder="https://..." />
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.5rem' }}>
                <SelectField
                  value={editType}
                  onChange={(v) => setEditType(v as SourceType)}
                  options={[
                    { value: 'rss', label: 'RSS Feed' },
                    { value: 'scraper', label: 'Web Scraper' },
                  ]}
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
                checked={source.enabled !== false}
                onChange={() => onToggle(source.id, !source.enabled)}
              />
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                  <span style={{ fontWeight: 600, fontSize: '0.875rem' }}>{source.name}</span>
                  <Badge variant={source.type === 'rss' ? 'accent' : 'default'} size="sm">{source.type}</Badge>
                </div>
                <div style={{ fontSize: '0.75rem', fontFamily: 'var(--font-mono)', color: 'var(--color-text-dim)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', marginTop: '0.125rem' }}>
                  {source.url}
                </div>
                {source.last_fetched_at && (
                  <div style={{ fontSize: '0.6875rem', color: 'var(--color-text-dim)', marginTop: '0.125rem' }}>
                    Last: {new Date(source.last_fetched_at).toLocaleString()}
                  </div>
                )}
              </div>
              <div style={{ display: 'flex', gap: '0.25rem', flexShrink: 0 }}>
                {onTest && <Button size="sm" variant="default" onClick={() => onTest(source.id)}>Test</Button>}
                <Button size="sm" variant="default" onClick={() => startEdit(source)}>Edit</Button>
                <Button size="sm" variant="danger" onClick={() => onDelete(source.id)}>✕</Button>
              </div>
            </div>
          )}
        </div>
      ))}

      {showNewForm ? (
        <div style={{ padding: '0.875rem 1rem', borderRadius: '14px', background: 'var(--color-glass-subtle)', border: '1px solid var(--color-glass-border)' }}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
            <TextField value={newName} onChange={setNewName} placeholder="Source name" />
            <TextField value={newUrl} onChange={setNewUrl} placeholder="https://..." />
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.5rem' }}>
              <SelectField
                value={newType}
                onChange={(v) => setNewType(v as SourceType)}
                options={[
                  { value: 'rss', label: 'RSS Feed' },
                  { value: 'scraper', label: 'Web Scraper' },
                ]}
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
        <Button variant="default" onClick={() => setShowNewForm(true)}>+ Add Source</Button>
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