import { GlassPanel, Badge, EmptyState, useLocale } from '../index';

// MCP server info type matching the API response
export interface MCPServerInfo {
  name: string;
  command: string;
  args: string[];
  enabled: boolean;
}

interface MCPServersTabProps {
  servers: MCPServerInfo[];
  loading: boolean;
}

export function MCPServersTab({ servers, loading }: MCPServersTabProps) {
  const { t } = useLocale();

  if (loading) {
    return (
      <div style={{ padding: '2rem', textAlign: 'center', color: 'var(--color-text-dim)' }}>
        {t('common.loading')}
      </div>
    );
  }

  if (!servers || servers.length === 0) {
    return (
      <div style={{ padding: '4rem 2rem', textAlign: 'center' }}>
        <EmptyState
          title={t('portal.no_mcp_servers')}
          hint={t('portal.no_mcp_servers.hint')}
        />
      </div>
    );
  }

  return (
    <div style={{ padding: '2rem', maxWidth: '800px', margin: '0 auto' }}>
      <h2 style={{ marginBottom: '1.5rem' }}>{t('portal.mcp')}</h2>
      <div style={{ display: 'grid', gap: '1rem' }}>
        {servers.map((server) => (
          <GlassPanel key={server.name} style={{ padding: '1.25rem' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
              <div style={{ flex: 1, overflow: 'hidden' }}>
                <h3 style={{ margin: 0, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                  {server.name}
                </h3>
                <code
                  style={{
                    fontSize: '12px',
                    color: 'var(--color-text-dim)',
                    display: 'block',
                    marginTop: '0.5rem',
                    whiteSpace: 'nowrap',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                  }}
                >
                  {server.command}
                  {server.args.length > 0 && ` ${server.args.join(' ')}`}
                </code>
              </div>
              <Badge variant={server.enabled ? 'success' : 'default'}>
                {server.enabled ? 'Enabled' : 'Disabled'}
              </Badge>
            </div>
          </GlassPanel>
        ))}
      </div>
    </div>
  );
}
