import React, { useState } from 'react';
import {
  AppHeader,
  ToastProvider,
  ConfirmProvider,
  LocaleProvider,
  useLocale,
  useTheme,
  SplitPane,
  DataList,
  Badge,
  StreamPanel,
  StatCard,
  GlassPanel,
  TextField,
  Button,
  EmptyState,
  LoadingState,
  useStats,
  useSessions,
  useSessionStream,
  killSession,
  apiPost,
  apiGet,
  useApi,
  useToast,
  useConfirm,
  SettingsDialog,
  type SessionMetadata,
} from './index';
import type { DataListItem } from './components/data-display/DataList';
import { useSettings, type PortalSettings } from './components/feedback/SettingsDialog';
import { MarketplaceURLBar } from './components/layout/Marketplace/MarketplaceURLBar';
import { SkillsTab, type SkillInfo } from './components/SkillsTab';
import { MCPServersTab, type MCPServerInfo } from './components/MCPServersTab';
import { PluginsTab, type PluginInfo } from './components/PluginsTab';
import './styles/globals.css';

// ── Types ───────────────────────────────────

type TabId = 'start' | 'sessions' | 'projects' | 'skills' | 'mcp' | 'plugins' | 'marketplace';

// Project group type for grouping sessions by cwd
interface ProjectGroup {
  path: string;        // Full cwd path (empty string for "General")
  name: string;        // Last segment or "General"
  totalSessions: number;
  totalCost: number;
  isActive: boolean;   // Any session running?
  lastActive: number;  // Most recent session start_time
}

// ── Components ──────────────────────────────

function App() {
  return (
    <ToastProvider>
      <ConfirmProvider>
        <LocaleProvider>
          <AppContent />
        </LocaleProvider>
      </ConfirmProvider>
    </ToastProvider>
  );
}

function AppContent() {
  const { theme, setTheme } = useTheme();
  const { t, locale, setLocale } = useLocale();
  const { settings, saveSettings } = useSettings();
  const [activeTab, setActiveTab] = useState<TabId>('start');
  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(null);
  const [projectFilter, setProjectFilter] = useState<string>('');
  const [showSettings, setShowSettings] = useState(false);
  const [marketplaceBrowseView, setMarketplaceBrowseView] = useState(false);
  const [selectedSkill, setSelectedSkill] = useState<string | null>(null);
  const [selectedMcp, setSelectedMcp] = useState<string | null>(null);
  const [selectedPlugin, setSelectedPlugin] = useState<string | null>(null);

  // Fetch skills data for the Skills tab
  const { data: skills, loading: skillsLoading } = useApi<SkillInfo[]>('/api/skills');

  // Fetch MCP servers data for the MCP tab
  const { data: mcpServers, loading: mcpServersLoading } = useApi<MCPServerInfo[]>('/api/mcp/servers');

  // Fetch plugins data for the Plugins tab
  const { data: plugins, loading: pluginsLoading } = useApi<PluginInfo[]>('/api/plugins');

  // Callback to handle session creation and navigate to sessions tab
  const handleSessionCreated = (sessionId: string) => {
    setSelectedSessionId(sessionId);
    setActiveTab('sessions');
  };

  return (
    <div style={{ minHeight: '100vh', display: 'flex', flexDirection: 'column' }}>
      <AppHeader
        brand="Jenny Portal"
        tabs={[
          { id: 'start', label: t('portal.start') },
          { id: 'sessions', label: t('portal.sessions') },
          { id: 'projects', label: t('portal.projects') },
          { id: 'skills', label: t('portal.skills') },
          { id: 'mcp', label: t('portal.mcp') },
          { id: 'plugins', label: t('portal.plugins') },
          { id: 'marketplace', label: t('portal.marketplace') },
        ]}
        activeTab={activeTab}
        onTabChange={(id) => {
          setActiveTab(id as TabId);
          setMarketplaceBrowseView(false);
        }}
        theme={theme}
        onThemeChange={setTheme}
        locale={locale}
        onLocaleChange={setLocale}
      />

      <main style={{ flex: 1, overflow: 'hidden', display: 'flex', flexDirection: 'column', minHeight: 0, maxWidth: '72rem', width: '100%', margin: '0 auto', paddingLeft: 'var(--spacing-gutter)', paddingRight: 'var(--spacing-gutter)' }}>
        <TabContent
          activeTab={activeTab}
          onSessionCreated={handleSessionCreated}
          onOpenSettings={() => setShowSettings(true)}
          settings={settings}
          selectedSessionId={selectedSessionId}
          onSelectSession={setSelectedSessionId}
          projectFilter={projectFilter}
          onFilterChange={setProjectFilter}
          skills={skills ?? []}
          skillsLoading={skillsLoading}
          selectedSkill={selectedSkill}
          onSelectSkill={setSelectedSkill}
          mcpServers={mcpServers ?? []}
          mcpServersLoading={mcpServersLoading}
          selectedMcp={selectedMcp}
          onSelectMcp={setSelectedMcp}
          plugins={plugins ?? []}
          pluginsLoading={pluginsLoading}
          selectedPlugin={selectedPlugin}
          onSelectPlugin={setSelectedPlugin}
          marketplaceBrowseView={marketplaceBrowseView}
          onMarketplaceBrowse={setMarketplaceBrowseView}
        />
      </main>

      <SettingsDialog open={showSettings} onClose={() => setShowSettings(false)} settings={settings} onSave={saveSettings} />
    </div>
  );
}
interface StartTabProps {
  onSessionCreated: (sessionId: string) => void;
  onOpenSettings: () => void;
  settings: PortalSettings;
}

function StartTab({ onSessionCreated, onOpenSettings, settings }: StartTabProps) {
  const { t } = useLocale();
  const [prompt, setPrompt] = useState('');
  const [launching, setLaunching] = useState(false);
  const toast = useToast();
  const { data: stats, loading } = useStats();

  const formatCost = (cost: number) => {
    if (cost < 0.01) return '$0.00';
    return `${cost.toFixed(2)}`;
  };

  const handleLaunch = async () => {
    if (!prompt.trim() || launching) return;
    setLaunching(true);
    try {
      const body: Record<string, string> = {
        prompt: settings.promptPrefix ? `${settings.promptPrefix}\n${prompt}` : prompt,
      };
      if (settings.model) body.model = settings.model;
      if (settings.workingDir) body.cwd = settings.workingDir;
      const result = await apiPost<{ session_id: string }>('/api/sessions/start', body);
      setPrompt('');
      onSessionCreated(result.session_id);
    } catch (err) {
      toast.addToast({ kind: 'error', title: 'Failed to start session', message: err instanceof Error ? err.message : 'Unknown error' });
    } finally {
      setLaunching(false);
    }
  };

  return (
    <div className="page-stack page-shell" style={{ paddingTop: 'var(--spacing-section)' }}>
      {/* Stats row */}
      <section style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))', gap: '1rem' }}>
        <StatCard label="Total Sessions" value={loading ? '...' : String(stats?.total_sessions ?? 0)} />
        <StatCard label="Running" value={loading ? '...' : String(stats?.active_sessions ?? 0)} />
        <StatCard label="Total Cost" value={loading ? '...' : formatCost(stats?.total_cost_usd ?? 0)} />
        <StatCard label="Total Tokens" value={loading ? '...' : String(stats?.total_tokens ?? 0)} />
      </section>

      {/* New session panel */}
      <GlassPanel style={{ padding: 'var(--spacing-card)' }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--spacing-md)' }}>
          <h2 style={{ fontSize: '1.125rem', fontWeight: 800, letterSpacing: '-0.02em', margin: 0 }}>{t('portal.new_session')}</h2>
          <TextField
            value={prompt}
            onChange={setPrompt}
            placeholder="What can I help you with today?"
            multiline
            rows={4}
          />
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.75rem' }}>
            <Button variant="outline" onClick={onOpenSettings}>{t('portal.settings')}</Button>
            <Button variant="primary" disabled={!prompt.trim() || launching} onClick={handleLaunch}>
              {launching ? 'Launching...' : t('portal.launch')}
            </Button>
          </div>
        </div>
      </GlassPanel>

      {/* Quick start */}
      <section>
        <h3 className="section-label">{t('portal.recent_projects')}</h3>
        <p style={{ margin: '0.5rem 0 0', color: 'var(--color-text-muted)', fontSize: '0.875rem' }}>
          No projects yet — start a session to see your working directory here.
        </p>
      </section>
    </div>
  );
}

interface SessionsTabProps {
  selectedId: string | null;
  onSelect: (id: string | null) => void;
  projectFilter: string;
  onFilterChange: (filter: string) => void;
}

function SessionsTab({ selectedId: externalSelectedId, onSelect: externalOnSelect, projectFilter, onFilterChange }: SessionsTabProps) {
  const [internalSelectedId, setInternalSelectedId] = useState<string | null>(null);
  const { data: sessions, loading, refetch } = useSessions();
  const { t } = useLocale();

  const selectedId = externalSelectedId !== undefined ? externalSelectedId : internalSelectedId;
  const setSelectedId = externalOnSelect || setInternalSelectedId;

  const filteredSessions = projectFilter
    ? sessions?.filter((s: SessionMetadata) => s.cwd === projectFilter)
    : sessions;

  const sessionItems = filteredSessions?.map((s: SessionMetadata) => {
    const timeAgo = formatTimeAgo(s.start_time);
    return {
      id: s.id,
      title: s.cwd ? s.cwd.split(/[\\/]/).filter(Boolean).pop() || s.id : s.id,
      subtitle: `${timeAgo} · ${s.status}`,
      badge: <Badge variant={s.status === 'running' ? 'success' : 'default'} dot={s.status === 'running'}>{s.status}</Badge>,
    };
  }) ?? [];

  const handleSessionDeleted = () => {
    setSelectedId(null);
    refetch();
  };

  const handleShowAll = () => onFilterChange('');

  return (
    <MasterDetailPane
      title={t('portal.sessions')}
      items={sessionItems}
      selectedId={selectedId}
      onSelect={setSelectedId}
      loading={loading}
      emptyMessage="No sessions"
      selectionLabel="session"
      extraHeader={
        projectFilter ? (
          <Button variant="ghost" size="sm" onClick={handleShowAll}>
            {t('portal.show_all')}
          </Button>
        ) : null
      }
      emptyContent={
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', color: 'var(--color-text-dim)', fontSize: '0.875rem' }}>
          Select a session to view details
        </div>
      }
    >
      {(id) => <SessionDetail session={sessions?.find(s => s.id === id)} onDeleted={handleSessionDeleted} />}
    </MasterDetailPane>
  );
}

// Format timestamp to time ago string
function formatTimeAgo(timestamp: number): string {
  const seconds = Math.floor((Date.now() - timestamp) / 1000);
  if (seconds < 60) return 'just now';
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  return `${Math.floor(seconds / 86400)}d ago`;
}

function SessionDetail({ session, onDeleted }: { session?: SessionMetadata; onDeleted?: () => void }) {
  const [entries, setEntries] = useState<any[]>([]);
  const [showResumeInput, setShowResumeInput] = useState(false);
  const [resumePrompt, setResumePrompt] = useState('');
  const [resuming, setResuming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const toast = useToast();
  const { confirm } = useConfirm();
  const isRunning = session?.status === 'running';

  useSessionStream(session?.id ?? '', (data) => {
    setEntries(prev => [...prev, data]);
  });

  const formatCost = (cost?: number) => cost ? `${cost.toFixed(2)}` : '$0.00';

  const handleResume = async () => {
    if (!session?.id || !resumePrompt.trim() || resuming) return;
    setResuming(true);
    try {
      await apiPost(`/api/sessions/${session.id}/resume`, { prompt: resumePrompt });
      setShowResumeInput(false);
      setResumePrompt('');
      toast.addToast({ kind: 'success', title: 'Session resumed' });
    } catch (err) {
      toast.addToast({ kind: 'error', title: 'Failed to resume session', message: err instanceof Error ? err.message : 'Unknown error' });
    } finally {
      setResuming(false);
    }
  };

  const handleDelete = async () => {
    if (!session?.id || deleting) return;
    const confirmed = await confirm({
      title: 'Delete Session',
      message: 'Are you sure you want to delete this session? This action cannot be undone.',
      confirmLabel: 'Delete',
      cancelLabel: 'Cancel',
      dangerous: true,
    });
    if (!confirmed) return;
    setDeleting(true);
    try {
      await apiPost(`/api/sessions/${session.id}/delete`, {});
      toast.addToast({ kind: 'success', title: 'Session deleted' });
      onDeleted?.();
    } catch (err) {
      toast.addToast({ kind: 'error', title: 'Failed to delete session', message: err instanceof Error ? err.message : 'Unknown error' });
    } finally {
      setDeleting(false);
    }
  };

  return (
    <div className="detail-pane-layout">
      {/* Session header */}
      <header className="detail-pane-header">
        <div style={{ minWidth: 0 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.5rem', flexWrap: 'wrap' }}>
            <Badge variant={isRunning ? 'success' : 'default'} dot={isRunning}>
              {isRunning ? 'Running' : 'Exited'}
            </Badge>
            <span className="detail-pane-id">
              {session?.id ?? ''}
            </span>
          </div>
          <h2 className="detail-pane-title">
            {session?.cwd ?? 'Session'}
          </h2>
        </div>

        {/* Actions */}
        <div style={{ display: 'flex', gap: '0.5rem', flexShrink: 0, alignItems: 'center', flexWrap: 'wrap' }}>
          {isRunning ? (
            <Button variant="danger" size="sm" onClick={() => session?.id && killSession(session.id)}>Stop</Button>
          ) : showResumeInput ? (
            <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', flexWrap: 'wrap' }}>
              <TextField
                value={resumePrompt}
                onChange={setResumePrompt}
                placeholder="What should I do next?"
                style={{ minWidth: '180px', maxWidth: '400px', flex: 1 }}
              />
              <Button variant="primary" size="sm" disabled={!resumePrompt.trim() || resuming} onClick={handleResume}>
                {resuming ? 'Resuming...' : 'Resume'}
              </Button>
              <Button variant="ghost" size="sm" onClick={() => { setShowResumeInput(false); setResumePrompt(''); }}>Cancel</Button>
            </div>
          ) : (
            <Button variant="primary" size="sm" onClick={() => setShowResumeInput(true)}>Resume</Button>
          )}
          {!isRunning && (
            <Button variant="ghost" size="sm" onClick={handleDelete} disabled={deleting}>
              {deleting ? 'Deleting...' : 'Delete'}
            </Button>
          )}
        </div>
      </header>

      {/* Live region for screen readers */}
      <div aria-live="polite" aria-atomic="true" className="sr-only" style={{ position: 'absolute', left: '-9999px' }}>
        Session status: {isRunning ? 'Running' : 'Exited'}
      </div>

      {/* Scrollable content */}
      <div className="detail-pane-body">
        {/* Stats row */}
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(140px, 1fr))', gap: '1rem' }}>
          <StatCard label="Token Usage" value={session?.total_tokens ? String(session.total_tokens) : '0'} />
          <StatCard label="Cost" value={formatCost(session?.total_cost)} />
          <StatCard label="Model" value={session?.model ?? 'unknown'} />
        </div>

        <div className="divider" />

        <StreamPanel
          title="Transcript"
          sessionId={session?.id}
          stream="transcript"
          isRunning={isRunning}
          fetchStream={async () => entries.map(e => JSON.stringify(e)).join('\n')}
        />
      </div>
    </div>
  );
}

// ── Project Groups Hook ───────────────────────────────────────

/**
 * Group sessions by working directory (cwd) and compute aggregated stats.
 * Sessions with empty cwd are grouped under "General".
 */
function useProjectGroups(sessions: SessionMetadata[]): ProjectGroup[] {
  const groups: Record<string, SessionMetadata[]> = {};

  for (const s of sessions) {
    const key = s.cwd || '';
    if (!groups[key]) groups[key] = [];
    groups[key].push(s);
  }

  return Object.entries(groups)
    .map(([path, sessionList]) => ({
      path,
      name: path ? path.split(/[\\/]/).filter(Boolean).pop() || path : 'General',
      totalSessions: sessionList.length,
      totalCost: sessionList.reduce((sum, s) => sum + (s.total_cost || 0), 0),
      isActive: sessionList.some(s => s.status === 'running'),
      lastActive: Math.max(...sessionList.map(s => s.start_time)),
    }))
    .sort((a, b) => b.lastActive - a.lastActive);
}

// ── Projects Tab ─────────────────────────────────────────────

interface ProjectsTabProps {
  onNavigate: (tab: string) => void;
  onFilter: (cwd: string) => void;
}

function ProjectsTab({ onNavigate, onFilter }: ProjectsTabProps) {
  const { data: sessions, loading } = useSessions();
  const { t } = useLocale();

  const groups = useProjectGroups(sessions ?? []);

  if (loading) {
    return (
      <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'flex-start', paddingTop: '12vh' }}>
        <LoadingState label={t('common.loading')} variant="full" />
      </div>
    );
  }

  if (groups.length === 0) {
    return (
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'flex-start', paddingTop: '12vh', gap: '1.5rem' }}>
        <EmptyState
          title={t('portal.no_projects')}
          hint={t('portal.no_projects.hint')}
        />
        <Button variant="primary" onClick={() => onNavigate('start')}>
          {t('portal.start')}
        </Button>
      </div>
    );
  }

  return (
    <div className="page-stack page-shell" style={{ paddingTop: 'var(--spacing-section)' }}>
      {/* Page header */}
      <div style={{ marginBottom: 'var(--spacing-section)' }}>
        <h2 style={{ fontSize: '1.125rem', fontWeight: 800, letterSpacing: '-0.02em', marginBottom: '0.25rem' }}>{t('portal.projects')}</h2>
        <p style={{ margin: 0, color: 'var(--color-text-muted)', fontSize: '0.875rem' }}>
          {groups.length} {groups.length === 1 ? 'project' : 'projects'} grouped by working directory
        </p>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))', gap: 'var(--spacing-md)' }}>
        {groups.map(group => (
          <GlassPanel
            key={group.path}
            interactive
            style={{ padding: 'var(--spacing-md)', cursor: 'pointer' }}
            onClick={() => onFilter(group.path)}
          >
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 'var(--spacing-sm)' }}>
              <div style={{ overflow: 'hidden', minWidth: 0 }}>
                <h3 className="detail-pane-title">{group.name}</h3>
                <code className="detail-pane-id">
                  {group.path || '(no directory)'}
                </code>
              </div>
              <Badge variant={group.isActive ? 'success' : 'default'}>
                {group.isActive ? 'Active' : 'Idle'}
              </Badge>
            </div>
            <div style={{ display: 'flex', gap: 'var(--spacing-lg)' }}>
              <div style={{ fontSize: '12px' }}>
                <div style={{ color: 'var(--color-text-muted)', marginBottom: 'var(--spacing-xs)' }}>Sessions</div>
                <div style={{ fontWeight: 600 }}>{group.totalSessions}</div>
              </div>
              <div style={{ fontSize: '12px' }}>
                <div style={{ color: 'var(--color-text-muted)', marginBottom: 'var(--spacing-xs)' }}>Total Cost</div>
                <div style={{ fontWeight: 600 }}>${group.totalCost.toFixed(2)}</div>
              </div>
            </div>
          </GlassPanel>
        ))}
      </div>
    </div>
  );
}

// ── Marketplace Tab ────────────────────────────────────────────

// Marketplace item type from API
interface MarketplaceItem {
  name: string;
  type: 'skill' | 'plugin' | 'mcp';
  description: string;
  version: string;
  download_url: string;
}

interface MarketplaceTabProps {
  onNavigate: (tab: string) => void;
  onBrowse: () => void;
}

function MarketplaceTab({ onNavigate, onBrowse }: MarketplaceTabProps) {
  const { data: skills } = useApi<SkillInfo[]>('/api/skills');
  const { data: mcps } = useApi<MCPServerInfo[]>('/api/mcp/servers');
  const { data: plugins } = useApi<PluginInfo[]>('/api/plugins');
  const { t } = useLocale();

  const categories = [
    { id: 'skills', name: t('portal.skills'), count: skills?.length ?? 0, icon: '⚡', description: t('marketplace.skills_desc') },
    { id: 'mcp', name: t('portal.mcp'), count: mcps?.length ?? 0, icon: '🔌', description: t('marketplace.mcp_desc') },
    { id: 'plugins', name: t('portal.plugins'), count: plugins?.length ?? 0, icon: '🧩', description: t('marketplace.plugins_desc') },
  ];

  const totalInstalled = categories.reduce((sum, c) => sum + c.count, 0);

  if (totalInstalled === 0) {
    return (
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'flex-start', paddingTop: '12vh', gap: '1.5rem' }}>
        <EmptyState title={t('marketplace.empty_title')} hint={t('marketplace.empty_hint')} />
        <Button variant="primary" onClick={onBrowse}>Browse Marketplace</Button>
      </div>
    );
  }

  return (
    <div className="page-stack page-shell" style={{ paddingTop: 'var(--spacing-section)' }}>
      <div style={{ marginBottom: 'var(--spacing-lg)', display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 'var(--spacing-md)' }}>
        <div>
          <h2 style={{ fontSize: '1.125rem', fontWeight: 800, letterSpacing: '-0.02em', marginBottom: '0.25rem' }}>{t('portal.marketplace')}</h2>
          <p style={{ color: 'var(--color-text-muted)', margin: 0, fontSize: '0.875rem' }}>
            {t('portal.marketplace_description', { count: totalInstalled })}
          </p>
        </div>
        <Button variant="outline" onClick={onBrowse}>Browse</Button>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(260px, 1fr))', gap: 'var(--spacing-md)' }}>
        {categories.map((cat) => (
          <GlassPanel key={cat.id} interactive style={{ padding: 'var(--spacing-md)', cursor: 'pointer' }} onClick={() => onNavigate(cat.id)}>
            <div style={{ fontSize: '2rem', marginBottom: 'var(--spacing-sm)' }}>{cat.icon}</div>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 'var(--spacing-sm)' }}>
              <h3 className="detail-pane-title">{cat.name}</h3>
              <Badge variant={cat.count > 0 ? 'success' : 'default'}>{t('portal.installed', { count: cat.count })}</Badge>
            </div>
            <p style={{ margin: 'var(--spacing-sm) 0 0', color: 'var(--color-text-muted)', fontSize: '0.875rem' }}>
              {cat.description}
            </p>
          </GlassPanel>
        ))}
      </div>
    </div>
  );
}

// ── Marketplace Browse View ─────────────────────────────────────

const DEFAULT_MARKETPLACE_URL = 'https://raw.githubusercontent.com/ipy/jenny-marketplace/main/index.json';
const URL_STORAGE_KEY = 'jenny-marketplace-url';

interface MarketplaceBrowseViewProps {
  onBack: () => void;
}

function MarketplaceBrowseView({ onBack }: MarketplaceBrowseViewProps) {
  const { t } = useLocale();
  const toast = useToast();
  const [items, setItems] = React.useState<MarketplaceItem[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState<string | null>(null);
  const [installed, setInstalled] = React.useState<Set<string>>(new Set());
  const [installing, setInstalling] = React.useState<string | null>(null);
  const [sourceUrl, setSourceUrl] = React.useState<string>(() => {
    try { return localStorage.getItem(URL_STORAGE_KEY) || DEFAULT_MARKETPLACE_URL; }
    catch { return DEFAULT_MARKETPLACE_URL; }
  });

  const fetchFromUrl = async (url: string) => {
    setLoading(true);
    setError(null);
    try {
      const data = await apiGet<MarketplaceItem[]>(`/api/marketplace/browse?source=${encodeURIComponent(url)}`);
      setItems(data);
      try { localStorage.setItem(URL_STORAGE_KEY, url); } catch {}
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch marketplace');
    } finally {
      setLoading(false);
    }
  };

  React.useEffect(() => { fetchFromUrl(sourceUrl); }, []);

  React.useEffect(() => {
    const updateInstalled = async () => {
      try {
        const skills: SkillInfo[] = await apiGet('/api/skills');
        setInstalled(new Set<string>(skills.map((s) => s.name)));
      } catch {}
    };
    updateInstalled();
  }, []);

  const handleBrowse = () => fetchFromUrl(sourceUrl);
  const handleReset = () => {
    setSourceUrl(DEFAULT_MARKETPLACE_URL);
    fetchFromUrl(DEFAULT_MARKETPLACE_URL);
  };

  const handleInstall = async (item: MarketplaceItem) => {
    if (installed.has(item.name) || installing) return;
    setInstalling(item.name);
    try {
      await apiPost('/api/marketplace/install', {
        type: item.type,
        name: item.name,
        download_url: item.download_url,
      });
      setInstalled((prev) => new Set([...prev, item.name]));
      toast.addToast({ kind: 'success', title: 'Installed', message: `${item.name} has been installed.` });
    } catch (err) {
      const errMsg = err instanceof Error ? err.message : 'Unknown error';
      if (errMsg.includes('409') || errMsg.includes('already installed')) {
        setInstalled((prev) => new Set([...prev, item.name]));
        toast.addToast({ kind: 'info', title: 'Already installed', message: `${item.name} is already installed.` });
      } else {
        toast.addToast({ kind: 'error', title: 'Install failed', message: errMsg });
      }
    } finally {
      setInstalling(null);
    }
  };

  const getTypeIcon = (type: string) => {
    switch (type) {
      case 'skill': return '⚡';
      case 'mcp': return '🔌';
      case 'plugin': return '🧩';
      default: return '📦';
    }
  };

  const pageHeader = (
    <div style={{ marginBottom: 'var(--spacing-md)' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-md)', marginBottom: 'var(--spacing-md)', flexWrap: 'wrap' }}>
        <Button variant="ghost" size="sm" onClick={onBack}>← Back</Button>
        <h2 style={{ margin: 0, fontSize: '1.125rem', fontWeight: 800, letterSpacing: '-0.02em' }}>{t('portal.marketplace')}</h2>
      </div>
      <MarketplaceURLBar
        sourceUrl={sourceUrl}
        onSourceUrlChange={setSourceUrl}
        onBrowse={handleBrowse}
        onReset={handleReset}
        isDefaultUrl={sourceUrl === DEFAULT_MARKETPLACE_URL}
        loading={loading}
      />
    </div>
  );

  if (loading) {
    return (
      <div className="page-shell" style={{ paddingTop: 'var(--spacing-section)', flex: 1, display: 'flex', flexDirection: 'column' }}>
        {pageHeader}
        <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'flex-start', paddingTop: '12vh' }}>
          <LoadingState label="Loading marketplace…" variant="full" />
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="page-shell" style={{ paddingTop: 'var(--spacing-section)', flex: 1, display: 'flex', flexDirection: 'column' }}>
        {pageHeader}
        <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'flex-start', paddingTop: '12vh' }}>
          <EmptyState title="Marketplace source unreachable" hint={`Failed to fetch from:\n${sourceUrl}\n\nEnter a custom marketplace URL above or ensure the source is available.`} />
        </div>
      </div>
    );
  }

  if (items.length === 0) {
    return (
      <div className="page-shell" style={{ paddingTop: 'var(--spacing-section)', flex: 1, display: 'flex', flexDirection: 'column' }}>
        {pageHeader}
        <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'flex-start', paddingTop: '12vh' }}>
          <EmptyState title="No items available" hint="No items found in the marketplace. Try a different URL or wait for items to be added." />
        </div>
      </div>
    );
  }

  return (
    <div className="page-shell page-stack" style={{ paddingTop: 'var(--spacing-section)' }}>
      {pageHeader}
      <div style={{ display: 'grid', gap: 'var(--spacing-md)' }}>
        {items.map((item) => {
          const isInstalled = installed.has(item.name);
          const isInstalling = installing === item.name;
          return (
            <GlassPanel key={`${item.type}-${item.name}`} style={{ padding: 'var(--spacing-md)' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 'var(--spacing-md)' }}>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: 'var(--spacing-xs)', flexWrap: 'wrap' }}>
                    <span style={{ fontSize: '1.25rem' }}>{getTypeIcon(item.type)}</span>
                    <h3 className="detail-pane-title">{item.name}</h3>
                    <Badge variant="default">{item.version}</Badge>
                    <Badge variant="default">{item.type}</Badge>
                  </div>
                  <p className="line-clamp-2" style={{ margin: 0, color: 'var(--color-text-muted)', fontSize: '0.875rem' }}>
                    {item.description || '(no description)'}
                  </p>
                </div>
                <div style={{ flexShrink: 0 }}>
                  {isInstalled ? (
                    <Button variant="primary" size="sm" disabled>Installed</Button>
                  ) : (
                    <Button variant="primary" size="sm" disabled={isInstalling} onClick={() => handleInstall(item)}>
                      {isInstalling ? 'Installing...' : 'Install'}
                    </Button>
                  )}
                </div>
              </div>
            </GlassPanel>
          );
        })}
      </div>
    </div>
  );
}


// ── TabContent router ───────────────────────

interface TabContentProps {
  activeTab: TabId;
  onSessionCreated: (sessionId: string) => void;
  onOpenSettings: () => void;
  settings: PortalSettings;
  selectedSessionId: string | null;
  onSelectSession: (id: string | null) => void;
  projectFilter: string;
  onFilterChange: (filter: string) => void;
  skills: SkillInfo[];
  skillsLoading: boolean;
  selectedSkill: string | null;
  onSelectSkill: (id: string | null) => void;
  mcpServers: MCPServerInfo[];
  mcpServersLoading: boolean;
  selectedMcp: string | null;
  onSelectMcp: (id: string | null) => void;
  plugins: PluginInfo[];
  pluginsLoading: boolean;
  selectedPlugin: string | null;
  onSelectPlugin: (id: string | null) => void;
  marketplaceBrowseView: boolean;
  onMarketplaceBrowse: (v: boolean) => void;
}

function TabContent(props: TabContentProps) {
  switch (props.activeTab) {
    case 'start':
      return <StartTab onSessionCreated={props.onSessionCreated} onOpenSettings={props.onOpenSettings} settings={props.settings} />;
    case 'sessions':
      return <SessionsTab selectedId={props.selectedSessionId} onSelect={props.onSelectSession} projectFilter={props.projectFilter} onFilterChange={props.onFilterChange} />;
    case 'projects':
      return <ProjectsTab onNavigate={(tab) => {}} onFilter={(cwd) => { props.onFilterChange(cwd); props.onSelectSession(null); }} />;
    case 'skills':
      return <SkillsTab skills={props.skills} loading={props.skillsLoading} selectedId={props.selectedSkill} onSelect={props.onSelectSkill} />;
    case 'mcp':
      return <MCPServersTab servers={props.mcpServers} loading={props.mcpServersLoading} selectedId={props.selectedMcp} onSelect={props.onSelectMcp} />;
    case 'plugins':
      return <PluginsTab plugins={props.plugins} loading={props.pluginsLoading} selectedId={props.selectedPlugin} onSelect={props.onSelectPlugin} />;
    case 'marketplace':
      return props.marketplaceBrowseView
        ? <MarketplaceBrowseView onBack={() => props.onMarketplaceBrowse(false)} />
        : <MarketplaceTab onNavigate={(tab) => {}} onBrowse={() => props.onMarketplaceBrowse(true)} />;
    default:
      return null;
  }
}

// ── MasterDetailPane ────────────────────────

interface MasterDetailPaneProps<T extends string> {
  title: string;
  items: { id: T; title: string; subtitle?: string; badge?: React.ReactNode }[];
  selectedId: T | null;
  onSelect: (id: T) => void;
  loading?: boolean;
  emptyMessage?: string;
  selectionLabel?: string;
  extraHeader?: React.ReactNode;
  children: (selectedId: T) => React.ReactNode;
  emptyContent?: React.ReactNode;
}

function MasterDetailPane<T extends string>({
  title,
  items,
  selectedId,
  onSelect,
  loading,
  emptyMessage,
  selectionLabel,
  extraHeader,
  children,
  emptyContent,
}: MasterDetailPaneProps<T>) {
  return (
    <div className="master-detail-container">
      <SplitPane
        masterWidth="320px"
        master={
          <div className="master-pane">
            <div className="master-pane-header">
              <h2>{title}</h2>
              {extraHeader}
            </div>
            <div style={{ flex: 1, overflowY: 'auto', minHeight: 0 }}>
              {loading ? (
                <div style={{ padding: '1.5rem', textAlign: 'center' }}>
                  <LoadingState label={`Loading ${title.toLowerCase()}…`} variant="inline" />
                </div>
              ) : (
                <DataList
                  items={items}
                  selectedId={selectedId}
                  onSelect={onSelect}
                  selectionLabel={selectionLabel}
                  emptyMessage={emptyMessage}
                />
              )}
            </div>
          </div>
        }
        detail={
          selectedId ? children(selectedId) : (emptyContent ?? <DetailEmpty />) 
        }
      />
    </div>
  );
}

function DetailEmpty() {
  return (
    <div className="detail-pane-empty">
      Select an item to view details
    </div>
  );
}

export default App;