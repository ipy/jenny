import { useState, useEffect, useCallback } from 'react';
import { Portal } from './Portal';
import { Button } from '../ui-primitives/Button';
import { TextField, SelectField } from '../ui-primitives/FormField';
import { useLocale } from '../../i18n/locale-context';

// ── Types ───────────────────────────────────

export interface PortalSettings {
  model: string;
  workingDir: string;
  promptPrefix: string;
}

const STORAGE_KEY = 'jenny-portal-settings';

const DEFAULT_SETTINGS: PortalSettings = {
  model: '',
  workingDir: '',
  promptPrefix: '',
};

// ── useSettings Hook ─────────────────────────

/**
 * useSettings — Manages portal settings with localStorage persistence.
 * Handles SSR/incognito gracefully by catching localStorage errors.
 */
export function useSettings() {
  const [settings, setSettings] = useState<PortalSettings>(() => {
    try {
      const stored = localStorage.getItem(STORAGE_KEY);
      return stored ? { ...DEFAULT_SETTINGS, ...JSON.parse(stored) } : DEFAULT_SETTINGS;
    } catch {
      return DEFAULT_SETTINGS;
    }
  });

  const saveSettings = useCallback((s: PortalSettings) => {
    setSettings(s);
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(s));
    } catch {
      // localStorage unavailable (incognito, SSR) — silently ignore
    }
  }, []);

  return { settings, saveSettings };
}

// ── SettingsDialog ──────────────────────────

interface SettingsDialogProps {
  open: boolean;
  onClose: () => void;
}

const MODEL_OPTIONS = [
  { value: 'claude-sonnet-4', label: 'claude-sonnet-4' },
  { value: 'claude-opus-4', label: 'claude-opus-4' },
  { value: 'deepseek-v4-flash', label: 'deepseek-v4-flash' },
  { value: 'MiniMax-M2.7', label: 'MiniMax-M2.7' },
];

const backdropStyle: React.CSSProperties = {
  position: 'fixed',
  inset: 0,
  background: 'rgba(0, 0, 0, 0.6)',
  backdropFilter: 'blur(4px)',
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  zIndex: 1000,
};

const dialogStyle: React.CSSProperties = {
  background: 'var(--color-surface)',
  border: '1px solid var(--color-glass-border)',
  borderRadius: '16px',
  padding: '1.5rem',
  width: '100%',
  maxWidth: '480px',
  boxShadow: '0 8px 32px rgba(0, 0, 0, 0.4)',
  display: 'flex',
  flexDirection: 'column',
  gap: '1.25rem',
};

const headerStyle: React.CSSProperties = {
  display: 'flex',
  justifyContent: 'space-between',
  alignItems: 'center',
};

/**
 * SettingsDialog — Modal dialog for configuring portal settings.
 * AC1: Opens on click, has title, close button (✕), backdrop dismiss.
 * AC2: Contains Model dropdown, Working Directory input, Prompt Prefix textarea.
 * AC3: Save persists to localStorage; Cancel/discard discard changes.
 */
export function SettingsDialog({ open, onClose }: SettingsDialogProps) {
  const { t } = useLocale();
  const { settings, saveSettings } = useSettings();
  const [local, setLocal] = useState<PortalSettings>(settings);

  // Reset local state when dialog opens with fresh settings
  useEffect(() => {
    if (open) {
      setLocal(settings);
    }
  }, [open, settings]);

  if (!open) return null;

  const handleSave = () => {
    saveSettings(local);
    onClose();
  };

  const handleBackdropClick = (e: React.MouseEvent) => {
    if (e.target === e.currentTarget) {
      onClose();
    }
  };

  return (
    <Portal>
      <div style={backdropStyle} onClick={handleBackdropClick}>
        <div style={dialogStyle} onClick={(e) => e.stopPropagation()}>
          <div style={headerStyle}>
            <h2 style={{ margin: 0, fontSize: '1.125rem', fontWeight: 600 }}>{t('portal.settings')}</h2>
            <Button variant="ghost" size="sm" onClick={onClose} aria-label={t('common.close')}>
              ✕
            </Button>
          </div>

          <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
            {/* Model selector */}
            <SelectField
              id="settings-model"
              value={local.model}
              onChange={(value) => setLocal({ ...local, model: value })}
              options={MODEL_OPTIONS}
              placeholder="default (jenny env)"
            />

            {/* Working directory */}
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
              <TextField
                value={local.workingDir}
                onChange={(value) => setLocal({ ...local, workingDir: value })}
                placeholder="~/path/to/project"
              />
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setLocal({ ...local, workingDir: settings.workingDir })}
              >
                Use current
              </Button>
            </div>

            {/* Prompt prefix */}
            <TextField
              value={local.promptPrefix}
              onChange={(value) => setLocal({ ...local, promptPrefix: value })}
              placeholder="Add a default prefix to your prompts..."
              multiline
              rows={3}
            />
          </div>

          {/* Actions */}
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.75rem' }}>
            <Button variant="ghost" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button variant="primary" onClick={handleSave}>
              {t('common.save')}
            </Button>
          </div>
        </div>
      </div>
    </Portal>
  );
}
