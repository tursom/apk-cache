import { RefreshCw, Save, ShieldCheck } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { api } from '../api';
import { DataTable, ErrorMessage, Loading, Page, Panel, StatusBadge } from '../components';
import type { Setting, SettingSchema } from '../types';
import { lines } from '../utils';

type ConfigData = { settings: Setting[] };

const groupLabels: Record<string, string> = {
  runtime: '基础运行',
  cache: '缓存目录与 TTL',
  memory: '内存缓存',
  transport: '网络传输',
  apk: 'APK',
  apt: 'APT',
  proxy: '代理',
  hash_store: 'Hash Store'
};

export function ConfigPage({ toast }: { toast: (message: string, ok?: boolean) => void }) {
  const [settings, setSettings] = useState<Setting[]>([]);
  const [schema, setSchema] = useState<SettingSchema[]>([]);
  const [draft, setDraft] = useState<Record<string, unknown>>({});
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const load = async () => {
    setError('');
    setLoading(true);
    try {
      const [configData, schemaData] = await Promise.all([
        api<ConfigData>('/config'),
        api<{ items: SettingSchema[] }>('/config/schema')
      ]);
      setSettings(configData.settings || []);
      setSchema(schemaData.items || []);
      setDraft(Object.fromEntries((configData.settings || []).map(item => [item.key, item.value])));
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => { void load(); }, []);
  const schemaByKey = useMemo(() => new Map(schema.map(item => [item.key, item])), [schema]);
  const groups = useMemo(() => {
    const out = new Map<string, Setting[]>();
    for (const setting of settings) {
      const group = schemaByKey.get(setting.key)?.group || (setting.key.startsWith('hash_store.') ? 'hash_store' : setting.key.split('.')[0]);
      out.set(group, [...(out.get(group) || []), setting]);
    }
    return out;
  }, [settings, schemaByKey]);
  if (loading) return <Loading />;
  if (error) return <ErrorMessage message={error} />;
  const changedSettings = () => Object.fromEntries(settings
    .filter(setting => schemaByKey.get(setting.key)?.editable !== false)
    .filter(setting => JSON.stringify(draft[setting.key]) !== JSON.stringify(setting.value))
    .map(setting => [setting.key, draft[setting.key]]));
  const save = async () => {
    const changed = changedSettings();
    if (!Object.keys(changed).length) {
      toast('没有需要保存的变更');
      return;
    }
    const validation = await api<{ valid: boolean; restart_required?: string[] }>('/config/validate', {
      method: 'POST',
      body: { settings: changed }
    });
    const restart = validation.restart_required || [];
    const message = restart.length ? `以下配置需要重启后生效：${restart.join(', ')}\n确认保存？` : '确认保存配置？';
    if (!window.confirm(message)) return;
    const result = await api<{ restart_required?: string[] }>('/config', {
      method: 'PUT',
      body: { settings: changed }
    });
    toast(result.restart_required?.length ? '已保存，部分配置需重启' : '配置已保存');
    await load();
  };
  return (
    <Page title="配置" actions={
      <>
        <button type="button" onClick={() => api('/config/validate', { method: 'POST', body: { settings: changedSettings() } }).then(() => toast('配置校验通过')).catch(err => toast((err as Error).message, false))}><ShieldCheck size={15} />校验</button>
        <button className="primary" type="button" onClick={() => save().catch(err => toast((err as Error).message, false))}><Save size={15} />保存</button>
        <button type="button" onClick={() => api('/config/reload', { method: 'POST' }).then(() => { toast('运行配置已重载'); return load(); }).catch(err => toast((err as Error).message, false))}><RefreshCw size={15} />重载</button>
      </>
    }>
      {[...groups.entries()].map(([group, items]) => (
        <Panel key={group} title={groupLabels[group] || group}>
          <DataTable
            className="compact-table"
            columns={['配置项', '当前值', '来源', '生效方式']}
            rows={items.map(setting => {
              const itemSchema = schemaByKey.get(setting.key);
              return [
              <div className="setting-name">
                <strong>{itemSchema?.title || setting.key}</strong>
                <code>{setting.key}</code>
                {itemSchema?.description ? <span>{itemSchema.description}</span> : null}
              </div>,
              editor(setting, itemSchema, draft[setting.key], value => setDraft(prev => ({ ...prev, [setting.key]: value }))),
              setting.source || 'database',
              setting.restart_required ? <StatusBadge value="需重启" tone="warn" /> : <StatusBadge value="热更新" />
            ];
            })}
          />
        </Panel>
      ))}
    </Page>
  );
}

function editor(setting: Setting, schema: SettingSchema | undefined, value: unknown, onChange: (value: unknown) => void) {
  const disabled = setting.editable === false || schema?.editable === false;
  const control = schema?.control || '';
  if (setting.value_type === 'bool' || control === 'toggle') {
    return (
      <label className="check-row inline-check">
        <input type="checkbox" checked={Boolean(value)} disabled={disabled} onChange={event => onChange(event.target.checked)} />
        <span>{Boolean(value) ? '启用' : '禁用'}</span>
      </label>
    );
  }
  if (setting.value_type === 'int') {
    return <input type="number" min={0} value={Number(value || 0)} disabled={disabled} onChange={event => onChange(Number(event.target.value))} />;
  }
  if (setting.value_type === 'string[]') {
    return <textarea value={Array.isArray(value) ? value.join('\n') : ''} disabled={disabled} onChange={event => onChange(lines(event.target.value))} />;
  }
  return <input type={schema?.sensitive ? 'password' : 'text'} value={String(value ?? '')} disabled={disabled} onChange={event => onChange(event.target.value)} />;
}
