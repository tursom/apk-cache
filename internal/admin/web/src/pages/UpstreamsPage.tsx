import { Edit3, Plus, Power, TestTube2, Trash2 } from 'lucide-react';
import { FormEvent, useEffect, useState } from 'react';
import { api } from '../api';
import { Code, DataTable, ErrorMessage, Loading, Page, Panel, StatusBadge } from '../components';
import type { Upstream } from '../types';

const emptyUpstream: Upstream = {
  id: 0,
  name: '',
  url: '',
  proxy: '',
  kind: 'apk',
  enabled: true,
  priority: 100,
  created_at: '',
  updated_at: ''
};

export function UpstreamsPage({ toast }: { toast: (message: string, ok?: boolean) => void }) {
  const [items, setItems] = useState<Upstream[]>([]);
  const [editing, setEditing] = useState<Upstream>(emptyUpstream);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const load = async () => {
    setLoading(true);
    setError('');
    try {
      const data = await api<{ items: Upstream[] }>('/upstreams');
      setItems(data.items || []);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => { void load(); }, []);
  if (loading) return <Loading />;
  if (error) return <ErrorMessage message={error} />;
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const body = { ...editing, id: undefined };
    if (editing.id) {
      await api(`/upstreams/${editing.id}`, { method: 'PUT', body });
      toast('上游已更新');
    } else {
      await api('/upstreams', { method: 'POST', body });
      toast('上游已创建');
    }
    setEditing(emptyUpstream);
    await load();
  };
  const action = async (item: Upstream, kind: 'toggle' | 'delete' | 'test') => {
    if (kind === 'test') {
      const result = await api<{ ok: boolean; status_code?: number; duration_ms: number; error?: string }>(`/upstreams/${item.id}/test`, { method: 'POST' });
      toast(result.ok ? `连通：${result.status_code || 'ok'} / ${result.duration_ms} ms` : result.error || '测试失败', result.ok);
      return;
    }
    if (kind === 'delete') {
      if (!window.confirm('确认删除这个上游？')) return;
      await api(`/upstreams/${item.id}`, { method: 'DELETE' });
      toast('上游已删除');
    } else {
      await api(`/upstreams/${item.id}/${item.enabled ? 'disable' : 'enable'}`, { method: 'POST' });
      toast('上游状态已更新');
    }
    await load();
  };
  return (
    <Page title="上游" actions={<button type="button" onClick={() => setEditing(emptyUpstream)}><Plus size={15} />新增</button>}>
      <div className="split">
        <DataTable
          columns={['ID', '名称', 'URL', 'Proxy', 'Kind', '优先级', '状态', '操作']}
          rows={items.map(item => [
            String(item.id),
            item.name,
            <Code>{item.url}</Code>,
            <Code>{item.proxy}</Code>,
            item.kind,
            String(item.priority),
            item.enabled ? <StatusBadge value="enabled" /> : <StatusBadge value="disabled" tone="warn" />,
            <div className="cell-actions">
              <button type="button" onClick={() => setEditing(item)}><Edit3 size={14} />编辑</button>
              <button type="button" onClick={() => void action(item, 'test')}><TestTube2 size={14} />测试</button>
              <button type="button" onClick={() => void action(item, 'toggle')}><Power size={14} />{item.enabled ? '禁用' : '启用'}</button>
              <button className="danger" type="button" onClick={() => void action(item, 'delete')}><Trash2 size={14} />删除</button>
            </div>
          ])}
        />
        <Panel title={editing.id ? '编辑上游' : '新增上游'}>
          <form className="form" onSubmit={event => void submit(event)}>
            <label><span>名称</span><input value={editing.name} required onChange={event => setEditing({ ...editing, name: event.target.value })} /></label>
            <label><span>URL</span><input value={editing.url} placeholder="https://..." required onChange={event => setEditing({ ...editing, url: event.target.value })} /></label>
            <label><span>Proxy</span><input value={editing.proxy} placeholder="socks5://127.0.0.1:1080" onChange={event => setEditing({ ...editing, proxy: event.target.value })} /></label>
            <div className="field-row">
              <label><span>Kind</span><input value={editing.kind} onChange={event => setEditing({ ...editing, kind: event.target.value })} /></label>
              <label><span>优先级</span><input type="number" value={editing.priority} onChange={event => setEditing({ ...editing, priority: Number(event.target.value) })} /></label>
            </div>
            <label className="check-row"><input type="checkbox" checked={editing.enabled} onChange={event => setEditing({ ...editing, enabled: event.target.checked })} /><span>启用</span></label>
            <div className="actions"><button className="primary" type="submit">保存</button></div>
          </form>
        </Panel>
      </div>
    </Page>
  );
}
