import type { ReactNode } from 'react';
import { GlassPanel, Badge, SplitPane, DataList, LoadingState } from '../index';
import { useLocale } from '../i18n/locale-context';

export interface SkillInfo {
  name: string;
  description: string;
  path: string;
  activation_glob?: string;
}

interface SkillsTabProps {
  skills: SkillInfo[];
  loading: boolean;
  selectedId: string | null;
  onSelect: (id: string | null) => void;
}

export function SkillsTab({ skills, loading, selectedId, onSelect }: SkillsTabProps) {
  const { t } = useLocale();

  const items = skills.map((skill) => ({
    id: skill.name,
    title: skill.name,
    subtitle: skill.description || '(no description)',
    badge: <Badge variant="success" size="sm">Installed</Badge>,
  }));

  const selectedSkill = skills.find((s) => s.name === selectedId);

  return (
    <div className="master-detail-container">
      <SplitPane
        masterWidth="320px"
        master={
          <div className="master-pane">
            <div className="master-pane-header">
              <h2>{t('portal.skills')}</h2>
            </div>
            <div style={{ flex: 1, overflowY: 'auto', minHeight: 0 }}>
              {loading ? (
                <div style={{ padding: '1.5rem', textAlign: 'center' }}>
                  <LoadingState label="Loading skills…" variant="inline" />
                </div>
              ) : (
                <DataList items={items} selectedId={selectedId} onSelect={onSelect} selectionLabel="skill" emptyMessage="No skills installed" />
              )}
            </div>
          </div>
        }
        detail={
          selectedSkill ? (
            <>
              <header className="detail-pane-header">
                <div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.5rem', flexWrap: 'wrap' }}>
                    <span style={{ fontSize: '1.25rem' }}>⚡</span>
                    <h2 className="detail-pane-title">{selectedSkill.name}</h2>
                    <Badge variant="success" size="sm">Installed</Badge>
                  </div>
                  <code className="detail-pane-id">{selectedSkill.path}</code>
                </div>
              </header>
              <div className="detail-pane-body">
                <div>
                  <p className="section-label" style={{ marginBottom: '0.5rem' }}>Description</p>
                  <p style={{ color: 'var(--color-text)', fontSize: '0.9375rem', lineHeight: 1.6 }}>
                    {selectedSkill.description || '(no description)'}
                  </p>
                </div>
                {selectedSkill.activation_glob && (
                  <div>
                    <p className="section-label" style={{ marginBottom: '0.5rem' }}>Activation Glob</p>
                    <GlassPanel style={{ padding: '0.75rem 1rem' }}>
                      <code style={{ fontSize: '0.8125rem', color: 'var(--color-text)', fontFamily: 'var(--font-mono)' }}>
                        {selectedSkill.activation_glob}
                      </code>
                    </GlassPanel>
                  </div>
                )}
              </div>
            </>
          ) : (
            <DetailEmpty />
          )
        }
      />
    </div>
  );
}

function DetailEmpty() {
  return <div className="detail-pane-empty">Select a skill to view details</div>;
}