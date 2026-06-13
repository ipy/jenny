import React, { useState, useEffect } from 'react';
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
  SessionEventsPanel,
  StreamPanel,
  StatCard,
  GlassPanel,
  TextField,
  Button,
  EmptyState,
} from './index';
import './styles/globals.css';

// ── Types ───────────────────────────────────

type TabId = 'start' | 'sessions' | 'projects' | 'skills' | 'mcp' | 'plugins' | 'marketplace';

interface SessionMetadata {
  id: string;
  status: 'running' | 'exited';
  pid?: number;
  cwd: string;
  model: string;
  startTime: number;
  endTime?: number;
  totalTokens?: number;
  totalCost?: number;
}

// ── Components ──────────────────────────────

function App() {
  const { theme, setTheme } = useTheme();
  const { t } = useLocale();
  const [activeTab, setActiveTab] = useState<TabId>('start');

  return (
    <ToastProvider>
      <ConfirmProvider>
        <LocaleProvider>
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
              onTabChange={(id) => setActiveTab(id as TabId)}
              theme={theme}
              onThemeChange={setTheme}
            />

            <main style={{ flex: 1, position: 'relative', overflow: 'hidden' }}>
              {activeTab === 'start' && <StartTab />}
              {activeTab === 'sessions' && <SessionsTab />}
              {activeTab === 'projects' && <ProjectsTab />}
              {/* Other tabs placeholder */}
              {['skills', 'mcp', 'plugins', 'marketplace'].includes(activeTab) && (
                <div style={{ padding: '2rem', textAlign: 'center' }}>
                  <EmptyState title={t('portal.coming_soon')} hint={t('portal.coming_soon.hint')} />
                </div>
              )}
            </main>
          </div>
        </LocaleProvider>
      </ConfirmProvider>
    </ToastProvider>
  );
}

function StartTab() {
  const { t } = useLocale();
  const [prompt, setPrompt] = useState('');

  return (
    <div style={{ maxWidth: '800px', margin: '4rem auto', padding: '0 1.5rem', display: 'flex', flexDirection: 'column', gap: '2rem' }}>
      <section style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))', gap: '1rem' }}>
        <StatCard label="Total Sessions" value="128" />
        <StatCard label="Running" value="3" />
        <StatCard label="Total Cost" value="$12.45" />
        <StatCard label="Cache Hit" value="84%" />
      </section>

      <GlassPanel style={{ padding: '2rem' }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
          <h2 style={{ fontSize: '1.5rem', fontWeight: 600, margin: 0 }}>{t('portal.new_session')}</h2>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
            <TextField
              value={prompt}
              onChange={setPrompt}
              placeholder="What can I help you with today?"
              multiline
              rows={4}
            />
            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '1rem' }}>
              <Button variant="outline">Settings</Button>
              <Button variant="primary" disabled={!prompt.trim()}>{t('portal.launch')}</Button>
            </div>
          </div>
        </div>
      </GlassPanel>

      <section>
        <h3 className="section-label">{t('portal.recent_projects')}</h3>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: '1rem', marginTop: '1rem' }}>
          <GlassPanel interactive style={{ padding: '1rem' }}>
            <div style={{ fontWeight: 600 }}>jenny</div>
            <div style={{ fontSize: '12px', color: 'var(--color-text-muted)' }}>/Users/sin/work/agents/jenny</div>
          </GlassPanel>
          <GlassPanel interactive style={{ padding: '1rem' }}>
            <div style={{ fontWeight: 600 }}>glimpse-ui</div>
            <div style={{ fontSize: '12px', color: 'var(--color-text-muted)' }}>/Users/sin/work/glimpse-ui</div>
          </GlassPanel>
        </div>
      </section>
    </div>
  );
}

function SessionsTab() {
  const [selectedId, setSelectedId] = useState<string | null>(null);
  
  // Mock data
  const sessions = [
    { id: '018f3a8b-1b2c-7000-8000-000000000001', title: 'Refactor engine', subtitle: '3m ago · running', status: 'running' },
    { id: '018f3a8a-4d5e-7000-8000-000000000002', title: 'Fix CSS bug', subtitle: '15m ago · exited', status: 'exited' },
    { id: '018f3a89-2f3a-7000-8000-000000000003', title: 'Update README', subtitle: '1h ago · exited', status: 'exited' },
  ];

  return (
    <SplitPane
      masterWidth="320px"
      master={
        <DataList
          items={sessions.map(s => ({
            id: s.id,
            title: s.title,
            subtitle: s.subtitle,
            badge: <Badge variant={s.status === 'running' ? 'success' : 'default'} dot={s.status === 'running'}>{s.status}</Badge>
          }))}
          selectedId={selectedId}
          onSelect={setSelectedId}
          selectionLabel="session"
        />
      }
      detail={
        selectedId ? (
          <SessionDetail id={selectedId} isRunning={sessions.find(s => s.id === selectedId)?.status === 'running'} />
        ) : (
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', color: 'var(--color-text-dim)' }}>
            Select a session to view details
          </div>
        )
      }
    />
  );
}

function SessionDetail({ id, isRunning }: { id: string, isRunning?: boolean }) {
  return (
    <div style={{ padding: '1.5rem', display: 'flex', flexDirection: 'column', gap: '1.5rem', height: '100%', overflow: 'auto' }}>
      <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
        <div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.5rem' }}>
            <Badge variant={isRunning ? 'success' : 'default'} dot={isRunning}>{isRunning ? 'Running' : 'Exited'}</Badge>
            <span style={{ fontFamily: 'var(--font-mono)', fontSize: '12px', color: 'var(--color-text-muted)' }}>{id}</span>
          </div>
          <h2 style={{ margin: 0, fontSize: '1.25rem' }}>Refactor engine loop</h2>
        </div>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          {isRunning ? <Button variant="danger" size="sm">Stop</Button> : <Button variant="primary" size="sm">Resume</Button>}
          <Button variant="ghost" size="sm">Delete</Button>
        </div>
      </header>

      <div className="divider" />

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(140px, 1fr))', gap: '1rem' }}>
        <StatCard label="Token Usage" value="1.2k" />
        <StatCard label="Cost" value="$0.02" />
        <StatCard label="Turns" value="5" />
        <StatCard label="Model" value="claude-3-5-sonnet" />
      </div>

      <SessionEventsPanel
        sessionId={id}
        isRunning={isRunning}
        events={[
          { id: '1', kind: 'init', badge: 'INIT', preview: 'Session initialized in /work/jenny', timestamp_ms: Date.now() - 300000 },
          { id: '2', kind: 'user', badge: 'USER', preview: 'Refactor the main engine loop to support SSE streaming.', timestamp_ms: Date.now() - 290000 },
          { id: '3', kind: 'assistant', badge: 'AI', preview: 'I will start by analyzing the current engine implementation...', timestamp_ms: Date.now() - 280000 },
          { id: '4', kind: 'tool', badge: 'READ', preview: 'read_file(internal/agent/engine.go)', timestamp_ms: Date.now() - 250000, hasResult: true },
        ]}
      />

      <StreamPanel
        title="Transcript"
        sessionId={id}
        stream="transcript"
        isRunning={isRunning}
        fetchStream={async () => "Streaming log content..."}
      />
    </div>
  );
}

function ProjectsTab() {
  return (
    <div style={{ padding: '2rem', maxWidth: '1000px', margin: '0 auto' }}>
      <h2 style={{ marginBottom: '1.5rem' }}>Projects</h2>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(300px, 1fr))', gap: '1.5rem' }}>
        <GlassPanel interactive style={{ padding: '1.5rem' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
            <div>
              <h3 style={{ margin: 0 }}>jenny</h3>
              <code style={{ fontSize: '11px', color: 'var(--color-text-muted)' }}>/Users/sin/work/agents/jenny</code>
            </div>
            <Badge variant="success">Active</Badge>
          </div>
          <div style={{ marginTop: '1.5rem', display: 'flex', gap: '1rem' }}>
            <div style={{ fontSize: '12px' }}>
              <div style={{ color: 'var(--color-text-muted)' }}>Total Cost</div>
              <div style={{ fontWeight: 600 }}>$1.23</div>
            </div>
            <div style={{ fontSize: '12px' }}>
              <div style={{ color: 'var(--color-text-muted)' }}>Sessions</div>
              <div style={{ fontWeight: 600 }}>42</div>
            </div>
          </div>
        </GlassPanel>
      </div>
    </div>
  );
}

export default App;