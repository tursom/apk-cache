import {
  Boxes,
  ChartNoAxesCombined,
  Database,
  FileText,
  GitBranch,
  KeyRound,
  Lock,
  LogOut,
  Network,
  PackageOpen,
  ServerCog,
  Settings,
  ShieldCheck
} from 'lucide-react';
import { FormEvent, ReactNode, useEffect, useMemo, useState } from 'react';
import { api, setCSRF } from './api';
import { ErrorMessage, Toast } from './components';
import { APKPage } from './pages/APKPage';
import { APTPage } from './pages/APTPage';
import { CachePage } from './pages/CachePage';
import { ConfigPage } from './pages/ConfigPage';
import { DashboardPage } from './pages/DashboardPage';
import { LogsPage } from './pages/LogsPage';
import { ProxyPage } from './pages/ProxyPage';
import { SystemPage } from './pages/SystemPage';
import { UpstreamsPage } from './pages/UpstreamsPage';
import type { CurrentUser, SetupStatus, ToastState } from './types';

type RouteID = 'dashboard' | 'cache' | 'apk' | 'apt' | 'upstreams' | 'proxy' | 'config' | 'logs' | 'system';

const routes: Array<{ id: RouteID; label: string; icon: ReactNode }> = [
  { id: 'dashboard', label: '仪表盘', icon: <ChartNoAxesCombined size={17} /> },
  { id: 'cache', label: '缓存', icon: <Database size={17} /> },
  { id: 'apk', label: 'APK', icon: <PackageOpen size={17} /> },
  { id: 'apt', label: 'APT', icon: <Boxes size={17} /> },
  { id: 'upstreams', label: '上游', icon: <GitBranch size={17} /> },
  { id: 'proxy', label: '代理', icon: <Network size={17} /> },
  { id: 'config', label: '配置', icon: <Settings size={17} /> },
  { id: 'logs', label: '日志', icon: <FileText size={17} /> },
  { id: 'system', label: '系统', icon: <ServerCog size={17} /> }
];

const routeSet = new Set(routes.map(route => route.id));

export function App() {
  const [setupStatus, setSetupStatus] = useState<SetupStatus | null>(null);
  const [user, setUser] = useState<CurrentUser | null>(null);
  const [route, setRoute] = useState<RouteID>(routeFromPath());
  const [authError, setAuthError] = useState('');
  const [toast, setToast] = useState<ToastState | null>(null);
  const [passwordOpen, setPasswordOpen] = useState(false);

  const showToast = (message: string, ok = true) => {
    setToast({ message, tone: ok ? 'ok' : 'error' });
    window.setTimeout(() => setToast(null), 3200);
  };

  useEffect(() => {
    void boot();
    const onPop = () => setRoute(routeFromPath());
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, []);

  const boot = async () => {
    try {
      const status = await api<SetupStatus>('/setup/status');
      setSetupStatus(status);
      const me = await api<CurrentUser>('/auth/me').catch(() => null);
      if (me?.authenticated) {
        setCSRF(me.csrf_token);
        setUser(me);
        const next = routeFromPath();
        setRoute(next);
        if (location.pathname === '/admin/' || location.pathname === '/admin/login' || location.pathname === '/admin/setup') {
          history.replaceState({}, '', pathForRoute(next));
        }
      }
    } catch (err) {
      setAuthError((err as Error).message);
    }
  };

  const navigate = (next: RouteID) => {
    setRoute(next);
    history.pushState({}, '', pathForRoute(next));
  };

  const logout = async () => {
    await api('/auth/logout', { method: 'POST' }).catch(() => null);
    location.href = '/admin/login';
  };

  if (!user) {
    return (
      <>
        <AuthScreen setupStatus={setupStatus} onLogin={me => { setCSRF(me.csrf_token); setUser(me); navigate(routeFromPath()); }} error={authError} setError={setAuthError} />
        <Toast toast={toast} onClose={() => setToast(null)} />
      </>
    );
  }

  return (
    <>
      <header>
        <strong>APK Cache Admin</strong>
        <div className="user">
          <span>{user.username || 'admin'}</span>
          <button type="button" onClick={() => setPasswordOpen(true)}><KeyRound size={15} />修改密码</button>
          <button type="button" onClick={() => void logout()}><LogOut size={15} />退出</button>
        </div>
      </header>
      <div className="layout">
        <nav>
          {routes.map(item => (
            <button key={item.id} type="button" className={route === item.id ? 'active' : ''} onClick={() => navigate(item.id)}>
              {item.icon}<span>{item.label}</span>
            </button>
          ))}
        </nav>
        <main>
          <RouteView route={route} toast={showToast} />
        </main>
      </div>
      {passwordOpen ? <PasswordDialog onClose={() => setPasswordOpen(false)} toast={showToast} /> : null}
      <Toast toast={toast} onClose={() => setToast(null)} />
    </>
  );
}

function AuthScreen({
  setupStatus,
  onLogin,
  error,
  setError
}: {
  setupStatus: SetupStatus | null;
  onLogin: (user: CurrentUser) => void;
  error: string;
  setError: (message: string) => void;
}) {
  const setupRequired = setupStatus?.setup_required;
  const [bootstrapToken, setBootstrapToken] = useState('');
  const [username, setUsername] = useState('admin');
  const [password, setPassword] = useState('');
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setError('');
    try {
      if (setupRequired) {
        await api('/setup', { method: 'POST', body: { bootstrap_token: bootstrapToken, username, password } });
      }
      const me = await api<CurrentUser>('/auth/login', { method: 'POST', body: { username, password } });
      onLogin(me);
    } catch (err) {
      setError((err as Error).message);
    }
  };
  return (
    <section className="auth-shell">
      <form className="panel auth-card" onSubmit={event => void submit(event)}>
        <h1>APK Cache Admin</h1>
        <div className="body form">
          {setupRequired && !setupStatus?.bootstrap_configured ? <ErrorMessage message="需要配置 ADMIN_BOOTSTRAP_TOKEN 后重启服务" /> : null}
          {setupRequired ? (
            <label><span>Bootstrap Token</span><input type="password" value={bootstrapToken} autoComplete="off" onChange={event => setBootstrapToken(event.target.value)} /></label>
          ) : null}
          <label><span>用户名</span><input value={username} autoComplete="username" onChange={event => setUsername(event.target.value)} /></label>
          <label><span>密码</span><input type="password" value={password} autoComplete={setupRequired ? 'new-password' : 'current-password'} onChange={event => setPassword(event.target.value)} /></label>
          <button className="primary" type="submit" disabled={setupRequired && !setupStatus?.bootstrap_configured}>
            {setupRequired ? <ShieldCheck size={15} /> : <Lock size={15} />}
            {setupRequired ? '创建管理员并登录' : '登录'}
          </button>
          {error ? <ErrorMessage message={error} /> : null}
        </div>
      </form>
    </section>
  );
}

function PasswordDialog({ onClose, toast }: { onClose: () => void; toast: (message: string, ok?: boolean) => void }) {
  const [oldPassword, setOldPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [error, setError] = useState('');
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    try {
      await api('/auth/change-password', { method: 'POST', body: { old_password: oldPassword, new_password: newPassword } });
      toast('密码已修改');
      onClose();
    } catch (err) {
      setError((err as Error).message);
    }
  };
  return (
    <div className="modal-backdrop">
      <form className="panel modal form" onSubmit={event => void submit(event)}>
        <h2>修改密码</h2>
        <label><span>当前密码</span><input type="password" value={oldPassword} autoComplete="current-password" onChange={event => setOldPassword(event.target.value)} /></label>
        <label><span>新密码</span><input type="password" value={newPassword} autoComplete="new-password" onChange={event => setNewPassword(event.target.value)} /></label>
        {error ? <ErrorMessage message={error} /> : null}
        <div className="actions">
          <button type="button" onClick={onClose}>取消</button>
          <button className="primary" type="submit">保存</button>
        </div>
      </form>
    </div>
  );
}

function RouteView({ route, toast }: { route: RouteID; toast: (message: string, ok?: boolean) => void }) {
  const views = useMemo<Record<RouteID, ReactNode>>(() => ({
    dashboard: <DashboardPage />,
    cache: <CachePage toast={toast} />,
    apk: <APKPage toast={toast} />,
    apt: <APTPage toast={toast} />,
    upstreams: <UpstreamsPage toast={toast} />,
    proxy: <ProxyPage toast={toast} />,
    config: <ConfigPage toast={toast} />,
    logs: <LogsPage />,
    system: <SystemPage toast={toast} />
  }), [toast]);
  return views[route] || views.dashboard;
}

function routeFromPath(): RouteID {
  const raw = location.pathname.replace(/^\/admin\/?/, '').replace(/\/$/, '');
  if (!raw || raw === 'login' || raw === 'setup') return 'dashboard';
  return routeSet.has(raw as RouteID) ? raw as RouteID : 'dashboard';
}

function pathForRoute(route: RouteID) {
  return '/admin/' + route;
}
