import { RefreshCw } from 'lucide-react';
import { useEffect, useState } from 'react';
import { api } from '../api';
import { Code, DataTable, ErrorMessage, Loading, Metric, Page, Panel } from '../components';
import type { DashboardSummary } from '../types';
import { formatBytes, formatTime } from '../utils';

export function DashboardPage() {
  const [data, setData] = useState<DashboardSummary | null>(null);
  const [error, setError] = useState('');
  const load = async () => {
    setError('');
    try {
      setData(await api<DashboardSummary>('/dashboard/summary'));
    } catch (err) {
      setError((err as Error).message);
    }
  };
  useEffect(() => { void load(); }, []);
  if (error) return <ErrorMessage message={error} />;
  if (!data) return <Loading />;
  const requestStats = data.requests || {};
  const disk = data.disk_cache;
  return (
    <Page title="仪表盘" actions={<button type="button" onClick={load}><RefreshCw size={15} />刷新</button>}>
      <div className="grid metrics">
        <Metric label="服务状态" value={data.status} />
        <Metric label="APK 上游" value={`${data.apk_upstreams?.healthy || 0}/${data.apk_upstreams?.total || 0}`} />
        <Metric label="缓存对象" value={data.cache_objects || 0} />
        <Metric label="磁盘缓存" value={`${disk?.files || 0} files / ${formatBytes(disk?.size_bytes)}`} />
        <Metric label="内存缓存" value={data.memory_cache ? `${data.memory_cache.items} items / ${formatBytes(data.memory_cache.size)}` : 'disabled'} />
        <Metric label="请求错误" value={Number(requestStats.errors || 0)} />
        <Metric label="缓存命中" value={Number(requestStats.cache_hits || 0)} />
        <Metric label="活跃 CONNECT" value={data.connect?.active || 0} />
        <Metric label="Hash expected" value={String(data.hash_store?.expected_records || 0)} />
      </div>
      <Panel title="最近请求">
        <DataTable
          className="compact-table"
          columns={['时间', '方法', '协议', '状态', '缓存', '路径', '耗时']}
          rows={(data.recent_requests || []).map(item => [
            formatTime(item.ts),
            item.method,
            item.protocol,
            String(item.status_code),
            item.cache_status || '',
            <Code>{item.path}</Code>,
            `${item.duration_ms} ms`
          ])}
        />
      </Panel>
      <Panel title="最近错误">
        <DataTable
          className="compact-table"
          columns={['时间', '方法', '协议', '状态', '路径', '错误']}
          rows={(data.recent_errors || []).map(item => [
            formatTime(item.ts),
            item.method,
            item.protocol,
            String(item.status_code),
            <Code>{item.path}</Code>,
            item.error || ''
          ])}
        />
      </Panel>
    </Page>
  );
}
