import { Download } from 'lucide-react';
import { useEffect, useState } from 'react';
import { api, apiBlob } from '../api';
import { ErrorMessage, JsonBlock, Loading, Page, Panel } from '../components';
import { formatBytes } from '../utils';

export function SystemPage({ toast }: { toast: (message: string, ok?: boolean) => void }) {
  const [info, setInfo] = useState<unknown>(null);
  const [hash, setHash] = useState<unknown>(null);
  const [error, setError] = useState('');
  const load = async () => {
    setError('');
    try {
      const [systemInfo, hashStatus] = await Promise.all([api('/system/info'), api('/hash/status')]);
      setInfo(systemInfo);
      setHash(hashStatus);
    } catch (err) {
      setError((err as Error).message);
    }
  };
  useEffect(() => { void load(); }, []);
  if (error) return <ErrorMessage message={error} />;
  if (!info) return <Loading />;
  const record = info as Record<string, unknown>;
  const download = async () => {
    const blob = await apiBlob('/diagnostics/package', { method: 'POST' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `apk-cache-diagnostics-${new Date().toISOString().replaceAll(':', '')}.zip`;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
    toast(`诊断包已生成：${formatBytes(blob.size)}`);
  };
  return (
    <Page title="系统" actions={<button className="primary" type="button" onClick={() => void download()}><Download size={15} />下载诊断包</button>}>
      <div className="grid metrics">
        <Panel title="进程"><JsonBlock value={record.process} /></Panel>
        <Panel title="Go Runtime"><JsonBlock value={record.go} /></Panel>
        <Panel title="路径"><JsonBlock value={record.paths} /></Panel>
        <Panel title="功能开关"><JsonBlock value={record.features} /></Panel>
      </div>
      <div className="grid metrics">
        <Panel title="Hash Store"><JsonBlock value={hash} /></Panel>
        <Panel title="管理员"><JsonBlock value={record.admin} /></Panel>
        <Panel title="Build"><JsonBlock value={record.build} /></Panel>
      </div>
    </Page>
  );
}
