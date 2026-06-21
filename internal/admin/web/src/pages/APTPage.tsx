import { RefreshCw, Search, ShieldCheck } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { api } from '../api';
import { Code, DataTable, ErrorMessage, Loading, Page, Panel } from '../components';
import type { APTRecord } from '../types';
import { formatBytes, includesAll } from '../utils';

type APTTab = 'records' | 'indexes' | 'byhash' | 'validate';

export function APTPage({ toast }: { toast: (message: string, ok?: boolean) => void }) {
  const [tab, setTab] = useState<APTTab>('records');
  return (
    <Page title="APT" actions={<InlineTabs tab={tab} setTab={setTab} />}>
      {tab === 'indexes' ? <APTIndexes toast={toast} /> : null}
      {tab === 'records' ? <APTRecords byHashOnly={false} toast={toast} /> : null}
      {tab === 'byhash' ? <APTRecords byHashOnly toast={toast} /> : null}
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
