import { GlassPanel, Badge, SplitPane, DataList, LoadingState } from '../index';
import { useLocale } from '../i18n/locale-context';

export interface MCPServerInfo {
  name: string;
  command: string;
  args: string[];
  enabled: boolean;
}

interface MCPServersTabProps {
  servers: MCPServerInfo[];
  loading: boolean;
  selectedId: string | null;
  onSelect: (id: string | null) => void;
}

export function MCPServersTab({ servers, loading, selectedId, onSelect }: MCPServersTabProps) {
  const { t } = useLocale();

  const items = servers.map((server) => ({
    id: server.name,
    title: server.name,
    subtitle: server.enabled ? 'Enabled' : 'Disabled',
    badge: <Badge variant={server.enabled ? 'success' : 'default'} size="sm">{server.enabled ? 'Enabled' : 'Disabled'}</Badge>,
  }));

  const selectedServer = servers.find((s) => s.name === selectedId);

  return (
    <div className="master-detail-container">
      <SplitPane
        masterWidth="320px"
        master={
          <div className="master-pane">
            <div className="master-pane-header">
              <h2>{t('portal.mcp')}</h2>
            </div>
            <div style={{ flex: 1, overflowY: 'auto', minHeight: 0 }}>
              {loading ? (
                <div style={{ padding: '1.5rem', textAlign: 'center' }}>
                  <LoadingState label="Loading servers…" variant="inline" />
                </div>
              ) : (
                <DataList items={items} selectedId={selectedId} onSelect={onSelect} selectionLabel="server" emptyMessage="No MCP servers configured" />
              )}
            </div>
          </div>
        }
        detail={
          selectedServer ? (
            <>
              <header className="detail-pane-header">
                <div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.5rem', flexWrap: 'wrap' }}>
                    <span style={{ fontSize: '1.25rem' }}>🔌</span>
                    <h2 className="detail-pane-title">{selectedServer.name}</h2>
                    <Badge variant={selectedServer.enabled ? 'success' : 'default'} size="sm">
                      {selectedServer.enabled ? 'Enabled' : 'Disabled'}
                    </Badge>
                  </div>
                  <code className="detail-pane-id">
                    {selectedServer.command} {selectedServer.args.join(' ')}
                  </code>
                </div>
              </header>
              <div className="detail-pane-body">
                <div>
                  <p className="section-label" style={{ marginBottom: '0.5rem' }}>Command</p>
                  <GlassPanel style={{ padding: '0.75rem 1rem' }}>
                    <code style={{ fontSize: '0.8125rem', color: 'var(--color-text)', fontFamily: 'var(--font-mono)' }}>
                      {selectedServer.command}
                    </code>
                  </GlassPanel>
                </div>
                {selectedServer.args.length > 0 && (
                  <div>
                    <p className="section-label" style={{ marginBottom: '0.5rem' }}>Arguments</p>
                    <GlassPanel style={{ padding: '0.75rem 1rem' }}>
                      <code style={{ fontSize: '0.8125rem', color: 'var(--color-text)', fontFamily: 'var(--font-mono)' }}>
                        {selectedServer.args.join(' ')}
                      </code>
                    </GlassPanel>
                  </div>
                )}
                <div>
                  <p className="section-label" style={{ marginBottom: '0.5rem' }}>Status</p>
                  <p style={{ fontSize: '0.9375rem', color: 'var(--color-text)' }}>
                    This server is currently <strong>{selectedServer.enabled ? 'enabled' : 'disabled'}</strong>.
                  </p>
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
  return <div className="detail-pane-empty">Select a server to view details</div>;
}