import { Eye, Recycle, Search, Trash2, X, Zap } from 'lucide-react';
import { useEffect, useState } from 'react';
import { api } from '../api';
import { Code, DataTable, ErrorMessage, JsonBlock, Loading, Page, Panel, StatusBadge } from '../components';
import type { CacheObject } from '../types';
import { formatBytes, formatTime, lines } from '../utils';

type CacheFilters = {
  q: string;
  protocol: string;
  class: string;
  status: string;
  page: number;
  page_size: number;
};

const defaultFilters: CacheFilters = { q: '', protocol: '', class: '', status: '', page: 1, page_size: 50 };

type CacheObjectDetail = {
  object: CacheObject;
  hash: unknown;
};

type DetailState = {
  id: number;
  loading: boolean;
  data: CacheObjectDetail | null;
  error: string;
};

export function CachePage({ toast }: { toast: (message: string, ok?: boolean) => void }) {
  const [filters, setFilters] = useState<CacheFilters>(defaultFilters);
  const [draft, setDraft] = useState<CacheFilters>(defaultFilters);
  const [items, setItems] = useState<CacheObject[]>([]);
  const [total, setTotal] = useState(0);
  const [detail, setDetail] = useState<DetailState | null>(null);
  const [prewarm, setPrewarm] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const load = async () => {
    setLoading(true);
    setError('');
    try {
      const query = new URLSearchParams();
      Object.entries(filters).forEach(([key, value]) => {
        if (value) query.set(key, String(value));
      });
      const data = await api<{ items: CacheObject[]; total: number }>(`/cache/objects?${query.toString()}`);
      setItems(data.items || []);
      setTotal(data.total || 0);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => { void load(); }, [filters]);
  if (error) return <ErrorMessage message={error} />;
  const search = () => {
    setDetail(null);
    setFilters({ ...draft, page: 1 });
  };
  const showDetail = async (item: CacheObject) => {
    setDetail({ id: item.id, loading: true, data: null, error: '' });
    try {
      const data = await api<CacheObjectDetail>(`/cache/objects/${item.id}`);
      setDetail({ id: item.id, loading: false, data, error: '' });
    } catch (err) {
      const message = (err as Error).message;
      setDetail({ id: item.id, loading: false, data: null, error: message });
      toast(message, false);
    }
  };
  const batchDelete = async () => {
    const dry = await api<{ total: number }>('/cache/delete', { method: 'POST', body: { ...filters, dry_run: true } });
    if (!window.confirm(`匹配 ${dry.total} 个缓存对象，确认删除？`)) return;
    const result = await api<{ deleted: number }>('/cache/delete', { method: 'POST', body: { ...filters, dry_run: false } });
    toast(`已删除 ${result.deleted} 个缓存对象`);
    await load();
  };
  return (
    <Page title="缓存">
      <Panel title="过滤与操作">
        <div className="toolbar">
          <input placeholder="路径搜索" value={draft.q} onChange={event => setDraft({ ...draft, q: event.target.value })} />
          <select value={draft.protocol} onChange={event => setDraft({ ...draft, protocol: event.target.value })}>
            <option value="">全部协议</option>
            <option value="apk">apk</option>
            <option value="apt">apt</option>
            <option value="proxy">proxy</option>
          </select>
          <select value={draft.class} onChange={event => setDraft({ ...draft, class: event.target.value })}>
            <option value="">全部类型</option>
            <option value="index">index</option>
            <option value="package">package</option>
            <option value="other">other</option>
          </select>
          <select value={draft.status} onChange={event => setDraft({ ...draft, status: event.target.value })}>
            <option value="">全部状态</option>
            <option value="ok">ok</option>
            <option value="corrupted">corrupted</option>
            <option value="missing">missing</option>
          </select>
          <button type="button" onClick={search}><Search size={15} />搜索</button>
          <button className="danger" type="button" onClick={() => void batchDelete()}><Trash2 size={15} />批量删除</button>
          <button type="button" onClick={() => api<{ scanned: number }>('/cache/reconcile', { method: 'POST' }).then(result => { toast(`扫描完成：${result.scanned} 个文件`); return load(); }).catch(err => toast((err as Error).message, false))}><Recycle size={15} />扫描磁盘</button>
          <button type="button" onClick={() => api('/cache/memory/clear', { method: 'POST' }).then(() => toast('内存缓存已清空')).catch(err => toast((err as Error).message, false))}>清空内存缓存</button>
        </div>
        <div className="field">
          <span>预热 URL</span>
          <textarea value={prewarm} onChange={event => setPrewarm(event.target.value)} placeholder="http://..." />
        </div>
        <div className="actions">
          <button type="button" onClick={() => api<{ items: unknown[] }>('/cache/prewarm', { method: 'POST', body: { urls: lines(prewarm) } }).then(result => toast(`预热完成：${(result.items || []).length} 条`)).catch(err => toast((err as Error).message, false))}><Zap size={15} />预热</button>
        </div>
      </Panel>
      {loading ? <Loading /> : (
        <>
          <DataTable
            columns={['ID', '类型', 'Host', '路径', '大小', '缓存', '校验', '最后访问', '操作']}
            rows={items.map(item => [
              String(item.id),
              `${item.protocol}/${item.class}`,
              item.host,
              <Code>{item.request_path}</Code>,
              formatBytes(item.size_bytes),
              item.cache_status === 'ok' ? <StatusBadge value={item.cache_status} /> : <StatusBadge value={item.cache_status} tone="warn" />,
              item.validation_status,
              formatTime(item.last_accessed_at || item.updated_at),
              <div className="cell-actions">
                <button type="button" disabled={detail?.id === item.id && detail.loading} onClick={() => void showDetail(item)}>
                  <Eye size={14} />{detail?.id === item.id && detail.loading ? '加载中' : '详情'}
                </button>
                <button className="danger" type="button" onClick={() => void deleteOne(item, toast, load)}><Trash2 size={14} />删除</button>
              </div>
            ])}
          />
          <div className="actions">
            <span className="muted">共 {total} 条，第 {filters.page} 页</span>
            <button type="button" disabled={filters.page <= 1} onClick={() => setFilters({ ...filters, page: filters.page - 1 })}>上一页</button>
            <button type="button" disabled={filters.page * filters.page_size >= total} onClick={() => setFilters({ ...filters, page: filters.page + 1 })}>下一页</button>
          </div>
        </>
      )}
      {detail ? (
        <div className="modal-backdrop">
          <div className="panel modal cache-detail-modal">
            <div className="modal-head">
              <h2>缓存详情 #{detail.id}</h2>
              <button className="icon-button" type="button" onClick={() => setDetail(null)} aria-label="关闭详情">
                <X size={16} />
              </button>
            </div>
            {detail.loading ? <Loading /> : null}
            {detail.error ? <ErrorMessage message={detail.error} /> : null}
            {detail.data ? <JsonBlock value={detail.data} /> : null}
          </div>
        </div>
      ) : null}
    </Page>
  );
}

async function deleteOne(item: CacheObject, toast: (message: string, ok?: boolean) => void, reload: () => Promise<void>) {
  if (!window.confirm('确认删除这个缓存对象？')) return;
  await api(`/cache/objects/${item.id}`, { method: 'DELETE' });
  toast('缓存对象已删除');
  await reload();
}
