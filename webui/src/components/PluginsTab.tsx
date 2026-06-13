import { GlassPanel, Badge, EmptyState, useLocale } from '../index';

// Plugin info type matching the API response
export interface PluginInfo {
  name: string;
  version: string;
  description: string;
  root_path: string;
}

interface PluginsTabProps {
  plugins: PluginInfo[];
  loading: boolean;
}

export function PluginsTab({ plugins, loading }: PluginsTabProps) {
  const { t } = useLocale();

  if (loading) {
    return (
      <div style={{ padding: '2rem', textAlign: 'center', color: 'var(--color-text-dim)' }}>
        {t('common.loading')}
      </div>
    );
  }

  if (!plugins || plugins.length === 0) {
    return (
      <div style={{ padding: '4rem 2rem', textAlign: 'center' }}>
        <EmptyState
          title="No plugins installed"
          hint="Plugins extend jenny's capabilities. Install plugins to add new functionality."
        />
      </div>
    );
  }

  return (
    <div style={{ padding: '2rem', maxWidth: '800px', margin: '0 auto' }}>
      <h2 style={{ marginBottom: '1.5rem' }}>{t('portal.plugins')}</h2>
      <div style={{ display: 'grid', gap: '1rem' }}>
        {plugins.map((plugin) => (
          <GlassPanel key={plugin.root_path} style={{ padding: '1.25rem' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
              <div style={{ flex: 1, overflow: 'hidden' }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                  <h3 style={{ margin: 0, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                    {plugin.name}
                  </h3>
                  {plugin.version && (
                    <Badge variant="default">{plugin.version}</Badge>
                  )}
                </div>
                <p
                  style={{
                    margin: '0.5rem 0',
                    color: 'var(--color-text-muted)',
                    fontSize: '0.875rem',
                    overflow: 'hidden',
                    display: '-webkit-box',
                    WebkitLineClamp: 2,
                    WebkitBoxOrient: 'vertical',
                  }}
                >
                  {plugin.description || '(no description)'}
                </p>
                <code
                  style={{
                    fontSize: '11px',
                    color: 'var(--color-text-dim)',
                    display: 'block',
                    whiteSpace: 'nowrap',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                  }}
                >
                  {plugin.root_path}
                </code>
              </div>
              <Badge variant="success">Installed</Badge>
            </div>
          </GlassPanel>
        ))}
      </div>
    </div>
  );
}
