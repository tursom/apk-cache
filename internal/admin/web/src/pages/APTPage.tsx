import { Plus, RefreshCw, Save, Search, ShieldCheck, Trash2 } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { api } from '../api';
import { Code, DataTable, ErrorMessage, Loading, Page, Panel, StatusBadge } from '../components';
import type { APTMirror, APTRecord } from '../types';
import { formatBytes, includesAll } from '../utils';

type APTTab = 'records' | 'indexes' | 'byhash' | 'mirrors' | 'validate';

export function APTPage({ toast }: { toast: (message: string, ok?: boolean) => void }) {
  const [tab, setTab] = useState<APTTab>('records');
  return (
    <Page title="APT" actions={<InlineTabs tab={tab} setTab={setTab} />}>
      {tab === 'indexes' ? <APTIndexes toast={toast} /> : null}
      {tab === 'records' ? <APTRecords byHashOnly={false} toast={toast} /> : null}
      {tab === 'byhash' ? <APTRecords byHashOnly toast={toast} /> : null}
      {tab === 'mirrors' ? <APTMirrors toast={toast} /> : null}
      {tab === 'validate' ? <APTValidate toast={toast} /> : null}
    </Page>
  );
}

function InlineTabs({ tab, setTab }: { tab: APTTab; setTab: (tab: APTTab) => void }) {
  return (
    <div className="tabs-inline">
      <button className={tab === 'records' ? 'active' : ''} type="button" onClick={() => setTab('records')}>包记录</button>
      <button className={tab === 'indexes' ? 'active' : ''} type="button" onClick={() => setTab('indexes')}>Release/Packages</button>
      <button className={tab === 'byhash' ? 'active' : ''} type="button" onClick={() => setTab('byhash')}>by-hash</button>
      <button className={tab === 'mirrors' ? 'active' : ''} type="button" onClick={() => setTab('mirrors')}>镜像站</button>
      <button className={tab === 'validate' ? 'active' : ''} type="button" onClick={() => setTab('validate')}>校验</button>
    </div>
  );
}

function APTIndexes({ toast }: { toast: (message: string, ok?: boolean) => void }) {
  const [items, setItems] = useState<string[]>([]);
  const load = async () => setItems((await api<{ items: string[] }>('/apt/indexes')).items || []);
  useEffect(() => { void load(); }, []);
  return (
    <>
      <div className="toolbar">
        <button type="button" onClick={() => api('/apt/indexes/reload', { method: 'POST' }).then(() => { toast('APT 索引已重载'); return load(); }).catch(err => toast((err as Error).message, false))}><RefreshCw size={15} />重载索引</button>
      </div>
      <DataTable columns={['缓存路径']} rows={items.map(item => [<Code>{item}</Code>])} />
    </>
  );
}

function APTRecords({ byHashOnly, toast }: { byHashOnly: boolean; toast: (message: string, ok?: boolean) => void }) {
  const [items, setItems] = useState<APTRecord[]>([]);
  const [search, setSearch] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const load = async () => {
    setLoading(true);
    try {
      setItems((await api<{ items: APTRecord[] }>('/apt/records')).items || []);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => { void load(); }, []);
  const filtered = useMemo(() => items.filter(item => {
    const haystack = [item.source_index_cache_path, item.record_type, item.package_name, item.filename, item.sha256].join(' ');
    const isByHash = haystack.includes('/by-hash/');
    return (!byHashOnly || isByHash) && includesAll(haystack, search);
  }), [items, search, byHashOnly]);
  if (error) return <ErrorMessage message={error} />;
  if (loading) return <Loading />;
  return (
    <>
      <div className="toolbar">
        <input placeholder="package / filename / sha256" value={search} onChange={event => setSearch(event.target.value)} />
        <button type="button" onClick={() => void load()}><Search size={15} />刷新</button>
        <button type="button" onClick={() => api('/apt/indexes/reload', { method: 'POST' }).then(() => { toast('APT 索引已重载'); return load(); }).catch(err => toast((err as Error).message, false))}><RefreshCw size={15} />重载索引</button>
      </div>
      <DataTable
        columns={['Index', 'Type', 'Package', 'Filename', 'Size', 'SHA256']}
        rows={filtered.map(item => [
          <Code>{item.source_index_cache_path}</Code>,
          item.record_type,
          item.package_name || '',
          <Code>{item.filename}</Code>,
          formatBytes(item.size_bytes),
          <Code>{item.sha256 || ''}</Code>
        ])}
      />
    </>
  );
}

const emptyMirror: Omit<APTMirror, 'id' | 'created_at' | 'updated_at'> = {
  name: '',
  public_prefix: '/debian',
  upstream_url: '',
  proxy: '',
  enabled: true
};

function APTMirrors({ toast }: { toast: (message: string, ok?: boolean) => void }) {
  const [items, setItems] = useState<APTMirror[]>([]);
  const [draft, setDraft] = useState(emptyMirror);
  const [editingID, setEditingID] = useState<number | null>(null);
  const [sources, setSources] = useState('');
  const [error, setError] = useState('');
  const load = async () => {
    setError('');
    try {
      setItems((await api<{ items: APTMirror[] }>('/apt/mirrors')).items || []);
    } catch (err) {
      setError((err as Error).message);
    }
  };
  useEffect(() => { void load(); }, []);
  const reset = () => {
    setDraft(emptyMirror);
    setEditingID(null);
  };
  const save = async () => {
    if (editingID) {
      await api(`/apt/mirrors/${editingID}`, { method: 'PUT', body: draft });
      toast('APT 镜像站已保存');
    } else {
      await api('/apt/mirrors', { method: 'POST', body: draft });
      toast('APT 镜像站已添加');
    }
    reset();
    await load();
  };
  const edit = (item: APTMirror) => {
    setEditingID(item.id);
    setDraft({
      name: item.name,
      public_prefix: item.public_prefix,
      upstream_url: item.upstream_url,
      proxy: item.proxy,
      enabled: item.enabled
    });
  };
  const toggle = async (item: APTMirror) => {
    await api(`/apt/mirrors/${item.id}/${item.enabled ? 'disable' : 'enable'}`, { method: 'POST' });
    toast(item.enabled ? '镜像站已禁用' : '镜像站已启用');
    await load();
  };
  const remove = async (item: APTMirror) => {
    if (!window.confirm(`删除 ${item.public_prefix}？`)) return;
    await api(`/apt/mirrors/${item.id}`, { method: 'DELETE' });
    toast('镜像站已删除');
    await load();
  };
  const test = async (item: APTMirror) => {
    const path = window.prompt('测试路径', 'dists/stable/InRelease') || '';
    const result = await api<{ ok: boolean; target: string; status_code?: number; error?: string; duration_ms: number }>(`/apt/mirrors/${item.id}/test`, {
      method: 'POST',
      body: { path }
    });
    toast(result.ok ? `测试通过 ${result.status_code || ''}` : result.error || '测试失败', result.ok);
  };
  const sourcesList = async (item: APTMirror) => {
    const result = await api<{ line: string; base_url: string }>(`/apt/mirrors/${item.id}/sources-list`);
    setSources(result.line);
  };
  if (error) return <ErrorMessage message={error} />;
  return (
    <>
      <Panel title={editingID ? '编辑镜像站' : '新增镜像站'}>
        <div className="form">
          <div className="field-row">
            <label><span>名称</span><input value={draft.name} onChange={event => setDraft({ ...draft, name: event.target.value })} /></label>
            <label><span>本地路径前缀</span><input value={draft.public_prefix} onChange={event => setDraft({ ...draft, public_prefix: event.target.value })} /></label>
          </div>
          <label><span>上游 URL</span><input placeholder="https://deb.debian.org/debian" value={draft.upstream_url} onChange={event => setDraft({ ...draft, upstream_url: event.target.value })} /></label>
          <div className="field-row">
            <label><span>专用代理</span><input placeholder="socks5://127.0.0.1:1080" value={draft.proxy} onChange={event => setDraft({ ...draft, proxy: event.target.value })} /></label>
            <label className="check-row"><input type="checkbox" checked={draft.enabled} onChange={event => setDraft({ ...draft, enabled: event.target.checked })} /><span>启用</span></label>
          </div>
          <div className="actions">
            {editingID ? <button type="button" onClick={reset}>取消编辑</button> : null}
            <button className="primary" type="button" onClick={() => save().catch(err => toast((err as Error).message, false))}>{editingID ? <Save size={15} /> : <Plus size={15} />}{editingID ? '保存' : '添加'}</button>
          </div>
        </div>
      </Panel>
      {sources ? <Panel title="sources.list"><Code>{sources}</Code></Panel> : null}
      <DataTable
        columns={['名称', '本地路径', '上游 URL', '状态', '代理', '操作']}
        rows={items.map(item => [
          item.name,
          <Code>{item.public_prefix}</Code>,
          <Code>{item.upstream_url}</Code>,
          item.enabled ? <StatusBadge value="启用" /> : <StatusBadge value="禁用" tone="warn" />,
          item.proxy || '',
          <div className="cell-actions">
            <button type="button" onClick={() => edit(item)}>编辑</button>
            <button type="button" onClick={() => toggle(item).catch(err => toast((err as Error).message, false))}>{item.enabled ? '禁用' : '启用'}</button>
            <button type="button" onClick={() => test(item).catch(err => toast((err as Error).message, false))}>测试</button>
            <button type="button" onClick={() => sourcesList(item).catch(err => toast((err as Error).message, false))}>sources.list</button>
            <button className="danger" type="button" onClick={() => remove(item).catch(err => toast((err as Error).message, false))}><Trash2 size={15} />删除</button>
          </div>
        ])}
      />
    </>
  );
}

function APTValidate({ toast }: { toast: (message: string, ok?: boolean) => void }) {
  const [id, setID] = useState('');
  const [cachePath, setCachePath] = useState('');
  const [requestPath, setRequestPath] = useState('');
  const submit = async () => {
    const result = await api<{ valid: boolean }>('/apt/validate', {
      method: 'POST',
      body: { id: Number(id || 0), cache_path: cachePath, request_path: requestPath }
    });
    toast(result.valid ? '校验通过' : '校验失败', result.valid);
  };
  return (
    <Panel title="手动校验">
      <div className="form">
        <label><span>Cache Object ID</span><input type="number" min={1} value={id} onChange={event => setID(event.target.value)} /></label>
        <label><span>Cache Path</span><input value={cachePath} onChange={event => setCachePath(event.target.value)} /></label>
        <label><span>Request Path</span><input value={requestPath} onChange={event => setRequestPath(event.target.value)} /></label>
        <div className="actions"><button className="primary" type="button" onClick={() => void submit()}><ShieldCheck size={15} />校验</button></div>
      </div>
    </Panel>
  );
}
