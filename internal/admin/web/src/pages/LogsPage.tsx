import { RefreshCw } from 'lucide-react';
import { useEffect, useState } from 'react';
import { api } from '../api';
import { Code, DataTable, ErrorMessage, Loading, Page } from '../components';
import type { RequestLog } from '../types';
import { formatTime } from '../utils';

type LogTab = 'requests' | 'errors';

export function LogsPage() {
  const [tab, setTab] = useState<LogTab>('requests');
  const [limit, setLimit] = useState(200);
  const [items, setItems] = useState<RequestLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const load = async () => {
    setLoading(true);
    setError('');
    try {
      const path = tab === 'errors' ? '/logs/errors' : '/logs/requests';
      setItems((await api<{ items: RequestLog[] }>(`${path}?limit=${limit}`)).items || []);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => { void load(); }, [tab, limit]);
  return (
    <Page title="日志" actions={
      <>
        <div className="tabs-inline">
          <button className={tab === 'requests' ? 'active' : ''} type="button" onClick={() => setTab('requests')}>请求</button>
          <button className={tab === 'errors' ? 'active' : ''} type="button" onClick={() => setTab('errors')}>错误</button>
        </div>
        <select value={limit} onChange={event => setLimit(Number(event.target.value))}>
          <option value={100}>100</option>
          <option value={200}>200</option>
          <option value={500}>500</option>
          <option value={1000}>1000</option>
        </select>
        <button type="button" onClick={() => void load()}><RefreshCw size={15} />刷新</button>
      </>
    }>
      {error ? <ErrorMessage message={error} /> : null}
      {loading ? <Loading /> : (
        <DataTable
          columns={['时间', '方法', '协议', 'Host', '状态', '缓存', '路径', '耗时', '错误']}
          rows={items.map(item => [
            formatTime(item.ts),
            item.method,
            item.protocol,
            item.host || '',
            String(item.status_code),
            item.cache_status || '',
            <Code>{item.path}</Code>,
            `${item.duration_ms} ms`,
            item.error || ''
          ])}
        />
      )}
    </Page>
  );
}
