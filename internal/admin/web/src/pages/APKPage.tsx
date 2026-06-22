import { KeyRound, RefreshCw, Search } from 'lucide-react';
import { useEffect, useState } from 'react';
import { api } from '../api';
import { Code, DataTable, ErrorMessage, JsonBlock, Loading, Page, Pagination, Panel } from '../components';
import type { APKPackage, PaginatedResponse } from '../types';
import { formatBytes } from '../utils';

type APKTab = 'packages' | 'indexes' | 'keys';

export function APKPage({ toast }: { toast: (message: string, ok?: boolean) => void }) {
  const [tab, setTab] = useState<APKTab>('packages');
  const [search, setSearch] = useState('');
  return (
    <Page title="APK" actions={<InlineTabs tab={tab} setTab={setTab} />}>
      {tab === 'packages' ? <APKPackages search={search} setSearch={setSearch} toast={toast} /> : null}
      {tab === 'indexes' ? <APKIndexes toast={toast} /> : null}
      {tab === 'keys' ? <APKKeys toast={toast} /> : null}
    </Page>
  );
}

function InlineTabs({ tab, setTab }: { tab: APKTab; setTab: (tab: APKTab) => void }) {
  return (
    <div className="tabs-inline">
      <button className={tab === 'packages' ? 'active' : ''} type="button" onClick={() => setTab('packages')}>包列表</button>
      <button className={tab === 'indexes' ? 'active' : ''} type="button" onClick={() => setTab('indexes')}>APKINDEX</button>
      <button className={tab === 'keys' ? 'active' : ''} type="button" onClick={() => setTab('keys')}>公钥</button>
    </div>
  );
}

function APKPackages({ search, setSearch, toast }: { search: string; setSearch: (value: string) => void; toast: (message: string, ok?: boolean) => void }) {
  const [items, setItems] = useState<APKPackage[]>([]);
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const pageSize = 50;
  const load = async (nextPage = page) => {
    setLoading(true);
    setError('');
    try {
      const query = new URLSearchParams({ page: String(nextPage), page_size: String(pageSize) });
      if (search) query.set('q', search);
      const data = await api<PaginatedResponse<APKPackage>>(`/apk/packages?${query.toString()}`);
      setItems(data.items || []);
      setTotal(data.total || 0);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => { void load(); }, [search, page]);
  if (error) return <ErrorMessage message={error} />;
  if (loading) return <Loading />;
  return (
    <>
      <div className="toolbar">
        <input placeholder="包名/版本" value={search} onChange={event => { setPage(1); setSearch(event.target.value); }} />
        <button type="button" onClick={() => { setPage(1); void load(1); }}><Search size={15} />搜索</button>
        <button type="button" onClick={() => api('/apk/indexes/reload', { method: 'POST' }).then(() => { toast('APK 索引已重载'); setPage(1); return load(1); }).catch(err => toast((err as Error).message, false))}><RefreshCw size={15} />重载索引</button>
      </div>
      <DataTable
        columns={['Index', 'Package', 'Version', 'Hash', 'Size']}
        rows={items.map(item => [
          <Code>{item.index_cache_path}</Code>,
          item.package_name,
          item.version,
          item.checksum_algorithm,
          formatBytes(item.size_bytes)
        ])}
      />
      <Pagination total={total} page={page} pageSize={pageSize} onPageChange={setPage} />
    </>
  );
}

function APKIndexes({ toast }: { toast: (message: string, ok?: boolean) => void }) {
  const [items, setItems] = useState<string[]>([]);
  const load = async () => setItems((await api<{ items: string[] }>('/apk/indexes')).items || []);
  useEffect(() => { void load(); }, []);
  return (
    <>
      <div className="toolbar">
        <button type="button" onClick={() => api('/apk/indexes/reload', { method: 'POST' }).then(() => { toast('APK 索引已重载'); return load(); }).catch(err => toast((err as Error).message, false))}><RefreshCw size={15} />重载索引</button>
      </div>
      <DataTable columns={['缓存路径']} rows={items.map(item => [<Code>{item}</Code>])} />
    </>
  );
}

function APKKeys({ toast }: { toast: (message: string, ok?: boolean) => void }) {
  const [data, setData] = useState<unknown>(null);
  const load = async () => setData(await api('/apk/keys'));
  useEffect(() => { void load(); }, []);
  return (
    <>
      <div className="toolbar">
        <button type="button" onClick={() => api('/apk/keys/reload', { method: 'POST' }).then(() => { toast('APK 公钥已重载'); return load(); }).catch(err => toast((err as Error).message, false))}><KeyRound size={15} />重载公钥</button>
      </div>
      <Panel title="公钥状态"><JsonBlock value={data} /></Panel>
    </>
  );
}
