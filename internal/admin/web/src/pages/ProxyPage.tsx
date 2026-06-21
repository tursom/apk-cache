import { Plus, Save, Trash2 } from 'lucide-react';
import { useEffect, useState } from 'react';
import { api } from '../api';
import { DataTable, ErrorMessage, JsonBlock, Loading, Page, Panel, StatusBadge } from '../components';
import type { ProxyHostRule } from '../types';

type ProxyStatus = {
  enabled: boolean;
  allow_connect: boolean;
  cache_non_package_requests: boolean;
  upstream_proxy: string;
  allowed_hosts: string[];
  host_rules: ProxyHostRule[];
  host_rules_configured: boolean;
  connect: Record<string, unknown>;
};

export function ProxyPage({ toast }: { toast: (message: string, ok?: boolean) => void }) {
  const [data, setData] = useState<ProxyStatus | null>(null);
  const [rules, setRules] = useState<ProxyHostRule[]>([]);
  const [host, setHost] = useState('');
  const [description, setDescription] = useState('');
  const [error, setError] = useState('');
  const load = async () => {
    setError('');
    try {
      const [status, ruleData] = await Promise.all([
        api<ProxyStatus>('/proxy/status'),
        api<{ items: ProxyHostRule[] }>('/proxy/host-rules')
      ]);
      setData(status);
      setRules(ruleData.items || []);
    } catch (err) {
      setError((err as Error).message);
    }
  };
  useEffect(() => { void load(); }, []);
  if (error) return <ErrorMessage message={error} />;
  if (!data) return <Loading />;
  const save = async () => {
    await api('/proxy/config', {
      method: 'PUT',
      body: {
        enabled: data.enabled,
        allow_connect: data.allow_connect,
        cache_non_package_requests: data.cache_non_package_requests,
        upstream_proxy: data.upstream_proxy
      }
    });
    toast('代理配置已保存');
    await load();
  };
  const addRule = async () => {
    await api('/proxy/host-rules', { method: 'POST', body: { host, description, enabled: true } });
    setHost('');
    setDescription('');
    toast('白名单已添加');
    await load();
  };
  const setRuleEnabled = async (rule: ProxyHostRule, enabled: boolean) => {
    await api(`/proxy/host-rules/${rule.id}/${enabled ? 'enable' : 'disable'}`, { method: 'POST' });
    toast(enabled ? '白名单已启用' : '白名单已禁用');
    await load();
  };
  const deleteRule = async (rule: ProxyHostRule) => {
    if (!window.confirm(`删除 ${rule.host}？`)) return;
    await api(`/proxy/host-rules/${rule.id}`, { method: 'DELETE' });
    toast('白名单已删除');
    await load();
  };
  return (
    <Page title="代理" actions={<button className="primary" type="button" onClick={() => void save()}><Save size={15} />保存</button>}>
      <div className="split">
        <Panel title="代理配置">
          <div className="form">
            <label className="check-row"><input type="checkbox" checked={data.enabled} onChange={event => setData({ ...data, enabled: event.target.checked })} /><span>启用通用代理</span></label>
            <label className="check-row"><input type="checkbox" checked={data.allow_connect} onChange={event => setData({ ...data, allow_connect: event.target.checked })} /><span>允许 CONNECT</span></label>
            <label className="check-row"><input type="checkbox" checked={data.cache_non_package_requests} onChange={event => setData({ ...data, cache_non_package_requests: event.target.checked })} /><span>缓存非包请求</span></label>
            <label><span>上游代理</span><input value={data.upstream_proxy || ''} placeholder="socks5://127.0.0.1:1080" onChange={event => setData({ ...data, upstream_proxy: event.target.value })} /></label>
            <div className="field">
              <span>白名单模式</span>
              {data.host_rules_configured ? <StatusBadge value={`${data.allowed_hosts.length} 个启用 Host`} /> : <StatusBadge value="未配置，允许全部" tone="warn" />}
            </div>
          </div>
        </Panel>
        <Panel title="运行状态"><JsonBlock value={data.connect} /></Panel>
      </div>
      <Panel title="代理网站白名单">
        <div className="toolbar">
          <input placeholder="deb.debian.org" value={host} onChange={event => setHost(event.target.value)} />
          <input placeholder="备注" value={description} onChange={event => setDescription(event.target.value)} />
          <button type="button" onClick={() => addRule().catch(err => toast((err as Error).message, false))}><Plus size={15} />添加</button>
        </div>
        <DataTable
          columns={['Host', '状态', '备注', '更新时间', '操作']}
          rows={rules.map(rule => [
            rule.host,
            rule.enabled ? <StatusBadge value="启用" /> : <StatusBadge value="禁用" tone="warn" />,
            rule.description || '',
            rule.updated_at,
            <div className="cell-actions">
              <button type="button" onClick={() => setRuleEnabled(rule, !rule.enabled).catch(err => toast((err as Error).message, false))}>{rule.enabled ? '禁用' : '启用'}</button>
              <button className="danger" type="button" onClick={() => deleteRule(rule).catch(err => toast((err as Error).message, false))}><Trash2 size={15} />删除</button>
            </div>
          ])}
        />
      </Panel>
    </Page>
  );
}
