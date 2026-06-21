import { Save } from 'lucide-react';
import { useEffect, useState } from 'react';
import { api } from '../api';
import { ErrorMessage, JsonBlock, Loading, Page, Panel } from '../components';
import { lines } from '../utils';

type ProxyStatus = {
  enabled: boolean;
  allow_connect: boolean;
  cache_non_package_requests: boolean;
  upstream_proxy: string;
  allowed_hosts: string[];
  connect: Record<string, unknown>;
};

export function ProxyPage({ toast }: { toast: (message: string, ok?: boolean) => void }) {
  const [data, setData] = useState<ProxyStatus | null>(null);
  const [error, setError] = useState('');
  const load = async () => {
    setError('');
    try {
      setData(await api<ProxyStatus>('/proxy/status'));
    } catch (err) {
      setError((err as Error).message);
    }
  };
  useEffect(() => { void load(); }, []);
  if (error) return <ErrorMessage message={error} />;
  if (!data) return <Loading />;
  const save = async () => {
    await api('/proxy/config', { method: 'PUT', body: data });
    toast('代理配置已保存');
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
            <label><span>允许 Host</span><textarea value={(data.allowed_hosts || []).join('\n')} onChange={event => setData({ ...data, allowed_hosts: lines(event.target.value) })} /></label>
          </div>
        </Panel>
        <Panel title="运行状态"><JsonBlock value={data.connect} /></Panel>
      </div>
    </Page>
  );
}
