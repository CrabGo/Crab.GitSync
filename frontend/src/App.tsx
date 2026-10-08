import { Fragment as ReactFragment, useEffect, useMemo, useRef, useState } from 'react'
import { GitService } from '../bindings/crab.gitsync'
import type { State } from '../bindings/crab.gitsync/models'
import type { Repository } from '../bindings/crab.gitsync/internal/gitengine/models'
import './App.css'

const initial: State = { busy: false, kind: '', phase: 'idle', root: '', current: '', visited: 0, completed: 0, total: 0, succeeded: 0, failed: 0, skipped: 0, startedAt: '', finishedAt: '', repositories: [], logs: [] }
const phases: Record<string, string> = { idle: '等待扫描', discovering: '正在发现仓库', inspecting: '正在读取仓库信息', fetching: '正在获取远端更新', done: '任务已完成', cancelled: '任务已取消', error: '任务失败' }
const levels: Record<string, string> = { info: '信息', success: '成功', warn: '警告', error: '错误' }
function Icon({ name, size = 18 }: { name: string, size?: number }) {
  const paths: Record<string, React.ReactNode> = {
    branch: <><path d="M6 6v12M6 12c0-4 12 0 12-6"/><circle cx="6" cy="4" r="2"/><circle cx="6" cy="20" r="2"/><circle cx="18" cy="4" r="2"/></>,
    folder: <path d="M3 7V5h6l2 2h10v12H3z"/>,
    scan: <><path d="M8 3H3v5m13-5h5v5M3 16v5h5m13-5v5h-5"/><circle cx="11" cy="11" r="4"/><path d="m14 14 3 3"/></>,
    download: <><path d="M12 3v12m-5-5 5 5 5-5M4 16v5h16v-5"/></>,
    search: <><circle cx="10" cy="10" r="6"/><path d="m15 15 5 5"/></>,
    terminal: <><rect x="3" y="4" width="18" height="16" rx="2"/><path d="m7 8 4 4-4 4m6 0h4"/></>,
    check: <path d="m5 12 4 4L19 6"/>,
    stop: <rect x="6" y="6" width="12" height="12" rx="2"/>,
    chevron: <path d="m9 5 7 7-7 7"/>,
    info: <><circle cx="12" cy="12" r="9"/><path d="M12 11v6m0-10v1"/></>,
    github: <><path d="M9 19c-4 1-4-2-6-2m13 4v-4c0-1-.4-2-1-2 4-.5 6-2 6-5 0-2-.6-3-2-4 .3-1 .3-2 0-3-2 0-3 1-4 1-2-.5-4-.5-6 0-1 0-2-1-4-1-.3 1-.3 2 0 3-1 1-2 2-2 4 0 3 2 4.5 6 5-.6.5-1 1-1 2v4"/></>,
  }
  return <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[name] || paths.info}</svg>
}
function remoteOf(repo: Repository) { return repo.remotes?.find(r => r.name === 'origin') || repo.remotes?.[0] }
function statusOf(repo: Repository): [string, string] {
  if (repo.fetchStatus === 'fetching') return ['正在获取', 'blue']
  if (repo.fetchStatus === 'error') return ['获取失败', 'red']
  if (repo.fetchStatus === 'cancelled') return ['已取消', 'amber']
  if (repo.fetchStatus === 'skipped') return ['已跳过', 'muted']
  if (repo.error) return ['信息异常', 'amber']
  if (repo.fetchStatus === 'success') return ['已获取更新', 'green']
  if (!repo.remotes?.length) return ['无远端', 'muted']
  if (repo.changed) return ['本地有修改', 'amber']
  return ['已就绪', 'green']
}

function App() {
  const [state, setState] = useState<State>(initial)
  const [path, setPath] = useState(() => localStorage.getItem('crab.scanPath') || '')
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState('all')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [expanded, setExpanded] = useState('')
  const [error, setError] = useState('')
  const [connected, setConnected] = useState(false)
  const [pending, setPending] = useState(false)
  const [help, setHelp] = useState(false)
  const [logFilter, setLogFilter] = useState('all')
  const [logCutoff, setLogCutoff] = useState(0)
  const [follow, setFollow] = useState(true)
  const logBody = useRef<HTMLDivElement>(null)
  const logSection = useRef<HTMLElement>(null)
  const allCheck = useRef<HTMLInputElement>(null)

  useEffect(() => {
    let stopped = false
    let timer: ReturnType<typeof setTimeout>
    const poll = async () => {
      let delay = 1000
      try {
        const snapshot = await GitService.GetState()
        if (!stopped) { setState(snapshot); setConnected(true) }
        delay = snapshot.busy ? 300 : 1000
      } catch { if (!stopped) setConnected(false) }
      if (!stopped) timer = setTimeout(poll, delay)
    }
    void poll()
    return () => { stopped = true; clearTimeout(timer) }
  }, [])
  useEffect(() => { localStorage.setItem('crab.scanPath', path) }, [path])
  const repos = state.repositories || []
  const visible = useMemo(() => repos.filter(r => {
    const matches = `${r.name} ${r.path} ${r.branch} ${r.remotes?.map(x => x.url).join(' ')}`.toLowerCase().includes(query.toLowerCase())
    return matches && (filter === 'all' || (filter === 'github' && r.remotes?.some(x => x.github)) || (filter === 'changed' && r.changed > 0) || (filter === 'behind' && r.behind > 0))
  }), [repos, query, filter])
  const selectedPaths = repos.filter(r => selected.has(r.path)).map(r => r.path)
  const allSelected = visible.length > 0 && visible.every(r => selected.has(r.path))
  useEffect(() => { if (allCheck.current) allCheck.current.indeterminate = !allSelected && visible.some(r => selected.has(r.path)) }, [visible, selected, allSelected])
  const logs = (state.logs || []).filter(l => l.id > logCutoff && (logFilter === 'all' || l.level === logFilter))
  const lastLogID = logs.slice(-1)[0]?.id
  useEffect(() => { if (follow && logBody.current) logBody.current.scrollTop = logBody.current.scrollHeight }, [lastLogID, follow, logFilter])
  const busy = state.busy || pending
  const percent = state.total ? Math.round(state.completed / state.total * 100) : state.phase === 'done' ? 100 : 0
  const elapsed = state.startedAt ? Math.max(0, Math.round((new Date(state.finishedAt || Date.now()).getTime() - new Date(state.startedAt).getTime()) / 1000)) : 0
  const toggle = (value: string) => setSelected(old => { const next = new Set(old); next.has(value) ? next.delete(value) : next.add(value); return next })
  const toggleAll = () => setSelected(old => { const next = new Set(old); visible.forEach(r => allSelected ? next.delete(r.path) : next.add(r.path)); return next })
  const perform = async (action: () => Promise<unknown>) => {
    setPending(true); setError('')
    try { await action(); setState(await GitService.GetState()) } catch (e) { setError(String(e)) } finally { setPending(false) }
  }
  const scan = () => perform(async () => { await GitService.StartScan(path.trim()); setSelected(new Set()); setExpanded('') })
  const choose = () => perform(async () => { const chosen = await GitService.ChooseDirectory(); if (chosen) setPath(chosen) })
  const exportLogs = () => {
    const content = logs.map(l => `${l.time} [${levels[l.level] || l.level}] ${l.message}`).join('\n')
    const url = URL.createObjectURL(new Blob(['\ufeff' + content], { type: 'text/plain;charset=utf-8' }))
    const link = document.createElement('a'); link.href = url; link.download = 'crab-gitsync.log'; link.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  }

  return <div className="shell">
    <aside className="sidebar">
      <div className="brand"><span className="brand-icon"><Icon name="branch" size={24}/></span><div><strong>Crab<span>.GitSync</span></strong><small>仓库同步工具</small></div></div>
      <nav aria-label="主导航"><button className="nav-item active" onClick={() => window.scrollTo({ top: 0, behavior: 'smooth' })}><Icon name="branch"/>仓库工作台<span className="nav-count">{repos.length}</span></button><button className="nav-item" onClick={() => logSection.current?.scrollIntoView({ behavior: 'smooth' })}><Icon name="terminal"/>任务日志</button></nav>
      <div className="sidebar-note"><Icon name="github" size={21}/><strong>连接你的代码</strong><p>获取远端更新，让本地仓库信息保持最新。</p><span className="tag blue">仅获取 · Fetch</span></div>
      <div className="sidebar-bottom"><button className="nav-item" onClick={() => setHelp(!help)} aria-expanded={help}><Icon name="info"/>使用说明</button><div className="version">Crab.GitSync <span>v0.1.0</span></div></div>
    </aside>
    <main>
      <header className="page-header"><div><div className="breadcrumb">工作空间 <Icon name="chevron" size={12}/> 仓库工作台</div><h1>仓库工作台</h1><p>扫描本地 Git 仓库，一处查看状态与获取远端更新。</p></div><div className="connection"><i className={connected ? 'online' : ''}/>{connected ? 'Git 服务已连接' : '正在连接桌面服务'}</div></header>
      {help && <section className="help-panel"><strong>如何使用</strong><p>选择或输入一个目录，递归扫描其中的仓库。勾选仓库后点击「获取远端更新」，对各仓库的全部远端执行 fetch。认证使用本机 Git 配置，需提前完成 SSH 或凭据配置。</p><p>不会执行 pull、merge、push 或切换分支。扫描跳过 .git、node_modules、.venv，不跟随子目录符号链接。领先／落后数基于本地跟踪分支，fetch 后刷新。日志保留最近 500 条。</p><button onClick={() => setHelp(false)}>收起说明</button></section>}
      {error && <div className="error-banner" role="alert"><Icon name="info"/><span>{error}</span><button onClick={() => setError('')} aria-label="关闭错误">×</button></div>}
      <section className="scan-panel" aria-labelledby="scan-heading">
        <div className="section-label"><Icon name="folder"/><h2 id="scan-heading">扫描路径</h2><span>包含子目录</span></div>
        <div className="path-controls"><div className="path-input"><Icon name="folder"/><input aria-label="扫描路径" placeholder="选择或输入包含 Git 仓库的目录" value={path} disabled={busy} onChange={e => setPath(e.target.value)} onKeyDown={e => { if (e.key === 'Enter' && path.trim() && !busy && connected) void scan() }}/></div><button disabled={busy || !connected} onClick={() => void choose()}>选择目录</button><button className="primary" disabled={busy || !path.trim() || !connected} onClick={() => void scan()}><Icon name="scan"/>{state.busy && state.kind === 'scan' ? '扫描中…' : '扫描仓库'}</button></div>
        <div className="scan-hint">自动识别 Git 仓库、worktree 和裸仓库。扫描仅读取本地信息。</div>
        {state.phase !== 'idle' && <div className="progress-area"><div className="progress-heading"><span><i className={state.busy ? 'working-dot' : 'done-dot'}/>{phases[state.phase]}{state.phase === 'done' && state.failed > 0 ? '，部分项目需要查看日志' : ''}</span><span>{state.phase === 'discovering' ? `已遍历 ${state.visited} 个目录` : `${state.completed} / ${state.total}`}<b>{state.phase === 'discovering' ? '发现中' : `${percent}%`}</b></span></div><div className={`progress-track ${state.phase === 'discovering' ? 'indeterminate' : ''}`} role="progressbar" aria-label="任务进度" aria-valuemin={0} aria-valuemax={100} aria-valuenow={state.phase === 'discovering' ? undefined : percent}><div style={{ width: `${percent}%` }}/></div><div className="progress-bottom"><span title={state.current}>{state.current || state.root}</span><span>{elapsed} 秒{state.busy && <button className="cancel-btn" onClick={() => void perform(() => GitService.Cancel())}><Icon name="stop" size={13}/>取消任务</button>}</span></div></div>}
      </section>
      <div className="summary-row"><div><span className="stat-icon blue"><Icon name="branch"/></span><span>已发现仓库<strong>{repos.length}</strong></span></div><div><span className="stat-icon violet"><Icon name="github"/></span><span>GitHub 仓库<strong>{repos.filter(r => r.remotes?.some(x => x.github)).length}</strong></span></div><div><span className="stat-icon amber"><Icon name="folder"/></span><span>本地有修改<strong>{repos.filter(r => r.changed > 0).length}</strong></span></div><div><span className="stat-icon green"><Icon name="download"/></span><span>远端领先<strong>{repos.filter(r => r.behind > 0).length}</strong></span></div></div>
      <section className="repo-panel" aria-labelledby="repo-heading">
        <div className="repo-heading"><div><h2 id="repo-heading">仓库列表 <span>{repos.length}</span></h2><p>选择仓库获取更新，点击仓库名称查看详情。</p></div><button className="primary" disabled={busy || !selectedPaths.length || !connected} onClick={() => void perform(() => GitService.StartFetch(selectedPaths))}><Icon name="download"/>获取远端更新{selectedPaths.length > 0 && <span className="button-count">{selectedPaths.length}</span>}</button></div>
        <div className="table-toolbar"><div className="filters" role="group" aria-label="仓库筛选">{[['all', '全部仓库'], ['github', 'GitHub'], ['changed', '有修改'], ['behind', '有远端更新']].map(([key, label]) => <button key={key} className={filter === key ? 'selected' : ''} onClick={() => setFilter(key)} aria-pressed={filter === key}>{label}</button>)}</div><div className="search-input"><Icon name="search" size={16}/><input aria-label="搜索仓库" placeholder="搜索名称、分支或路径" value={query} onChange={e => setQuery(e.target.value)}/></div></div>
        <div className="table-scroll"><table><thead><tr><th className="check-col"><input ref={allCheck} aria-label="选择当前筛选的全部仓库" type="checkbox" checked={allSelected} disabled={busy || !visible.length} onChange={toggleAll}/></th><th>仓库 / 路径</th><th>分支</th><th>远端</th><th>工作区 / 提交差异</th><th>状态</th></tr></thead><tbody>{visible.map(repo => {
          const remote = remoteOf(repo); const [label, tone] = statusOf(repo)
          return <ReactFragment key={repo.path}><tr className={selected.has(repo.path) ? 'row-selected' : ''}><td><input type="checkbox" aria-label={`选择 ${repo.name}`} checked={selected.has(repo.path)} disabled={busy} onChange={() => toggle(repo.path)}/></td><td className="repo-name"><button onClick={() => setExpanded(expanded === repo.path ? '' : repo.path)} aria-expanded={expanded === repo.path}><Icon name="folder" size={17}/><strong>{repo.name}</strong><Icon name="chevron" size={12}/></button><small title={repo.path}>{repo.path}</small></td><td><span className="branch-name"><Icon name="branch" size={14}/>{repo.branch}</span>{repo.detached && <small>分离 HEAD</small>}{repo.bare && <small>裸仓库</small>}</td><td><span className="remote-name">{remote?.github && <Icon name="github" size={14}/>} {remote ? remote.github ? 'GitHub' : remote.name : '未配置'}</span><small title={remote?.url}>{remote?.url.replace(/^https?:\/\//, '').replace(/^git@/, '') || '添加远端后可获取更新'}</small></td><td><span className={repo.changed ? 'text-amber' : 'text-muted'}>{repo.bare ? '无工作区' : repo.changed ? `${repo.changed} 项修改` : '工作区干净'}</span><small>{repo.upstream ? <><span className={repo.ahead ? 'text-amber' : ''}>↑ {repo.ahead}</span> <span className={repo.behind ? 'text-blue' : ''}>↓ {repo.behind}</span></> : '无跟踪分支'}</small></td><td><span className={`tag ${tone}`} title={repo.error}><i/>{label}</span></td></tr>{expanded === repo.path && <tr className="detail-row"><td/><td colSpan={5}><div><span>最近提交</span><strong>{repo.lastCommit || '暂无提交'}</strong></div><div><span>跟踪分支</span><strong>{repo.upstream || '未设置'}</strong></div>{repo.remotes?.map(r => <div key={r.name}><span>远端 {r.name}</span><code>{r.url}</code></div>)}{repo.error && <p className="text-red">{repo.error}</p>}</td></tr>}</ReactFragment>
        })}</tbody></table>{visible.length === 0 && <div className="empty-state"><div className="empty-icon"><Icon name={repos.length ? 'search' : 'branch'} size={30}/></div><h3>{repos.length ? '没有匹配的仓库' : state.busy ? '正在寻找你的仓库' : state.phase === 'done' ? '此目录中没有有效的 Git 仓库' : '从一个本地目录开始'}</h3><p>{repos.length ? '试试其他关键词或筛选条件。' : '选择扫描路径，然后点击「扫描仓库」。仓库信息将在这里显示。'}</p></div>}</div>
        <div className="table-footer"><span>显示 {visible.length} 个仓库 · 已选择 {selectedPaths.length} 个</span><span><Icon name="info" size={13}/>提交差异基于最近一次 fetch</span></div>
      </section>
      <section className="log-panel" ref={logSection} aria-labelledby="log-heading"><div className="log-heading"><h2 id="log-heading"><Icon name="terminal"/>任务日志 <span>{logs.length}</span></h2><div><label className="follow-check"><input type="checkbox" checked={follow} onChange={e => setFollow(e.target.checked)}/>自动滚动</label><select aria-label="日志级别" value={logFilter} onChange={e => setLogFilter(e.target.value)}><option value="all">全部级别</option>{Object.entries(levels).map(([value, label]) => <option value={value} key={value}>{label}</option>)}</select><button className="text-button" disabled={!logs.length} onClick={exportLogs}>导出</button><button className="text-button" disabled={!logs.length} onClick={() => setLogCutoff((state.logs || []).slice(-1)[0]?.id || 0)}>清空显示</button></div></div><div className="log-body" ref={logBody} role="log" aria-label="任务日志内容">{logs.length ? logs.map(log => <div className="log-line" key={log.id}><time>{log.time}</time><span className={`log-level ${log.level}`}>{levels[log.level] || log.level}</span><span>{log.message}</span></div>) : <div className="log-placeholder"><span>›</span>等待任务开始，扫描和远端更新的日志将在这里显示。</div>}</div></section>
      <footer className="page-footer"><span><Icon name="check" size={13}/>Fetch 保留当前分支与工作区</span><span>使用本机 Git 与认证配置</span></footer>
    </main>
  </div>
}
export default App

