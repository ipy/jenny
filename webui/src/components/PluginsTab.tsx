import { GlassPanel, Badge, SplitPane, DataList, LoadingState } from '../index';
import { useLocale } from '../i18n/locale-context';

export interface PluginInfo {
  name: string;
  version: string;
  description: string;
  root_path: string;
}

interface PluginsTabProps {
  plugins: PluginInfo[];
  loading: boolean;
  selectedId: string | null;
  onSelect: (id: string | null) => void;
}

export function PluginsTab({ plugins, loading, selectedId, onSelect }: PluginsTabProps) {
  const { t } = useLocale();

  const items = plugins.map((plugin) => ({
    id: plugin.root_path,
    title: plugin.name,
    subtitle: plugin.version || '(no version)',
    badge: plugin.version ? <Badge variant="default" size="sm">{plugin.version}</Badge> : undefined,
  }));

  const selectedPlugin = plugins.find((p) => p.root_path === selectedId);

  return (
    <div className="master-detail-container">
      <SplitPane
        masterWidth="320px"
        master={
          <div className="master-pane">
            <div className="master-pane-header">
              <h2>{t('portal.plugins')}</h2>
            </div>
            <div style={{ flex: 1, overflowY: 'auto', minHeight: 0 }}>
              {loading ? (
                <div style={{ padding: '1.5rem', textAlign: 'center' }}>
                  <LoadingState label="Loading plugins…" variant="inline" />
                </div>
              ) : (
                <DataList items={items} selectedId={selectedId} onSelect={onSelect} selectionLabel="plugin" emptyMessage="No plugins installed" />
              )}
            </div>
          </div>
        }
        detail={
          selectedPlugin ? (
            <>
              <header className="detail-pane-header">
                <div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.5rem', flexWrap: 'wrap' }}>
                    <span style={{ fontSize: '1.25rem' }}>🧩</span>
                    <h2 className="detail-pane-title">{selectedPlugin.name}</h2>
                    {selectedPlugin.version && <Badge variant="default" size="sm">{selectedPlugin.version}</Badge>}
                    <Badge variant="success" size="sm">Installed</Badge>
                  </div>
                  <code className="detail-pane-id">{selectedPlugin.root_path}</code>
                </div>
              </header>
              <div className="detail-pane-body">
                <div>
                  <p className="section-label" style={{ marginBottom: '0.5rem' }}>Description</p>
                  <p style={{ color: 'var(--color-text)', fontSize: '0.9375rem', lineHeight: 1.6 }}>
                    {selectedPlugin.description || '(no description)'}
                  </p>
                </div>
                <div>
                  <p className="section-label" style={{ marginBottom: '0.5rem' }}>Root Path</p>
                  <GlassPanel style={{ padding: '0.75rem 1rem' }}>
                    <code style={{ fontSize: '0.8125rem', color: 'var(--color-text)', fontFamily: 'var(--font-mono)' }}>
                      {selectedPlugin.root_path}
                    </code>
                  </GlassPanel>
                </div>
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
  return <div className="detail-pane-empty">Select a plugin to view details</div>;
}