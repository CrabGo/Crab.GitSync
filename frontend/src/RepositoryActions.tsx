import { useEffect, useRef, useState } from 'react'
import { GitService } from '../bindings/crab.gitsync'
import type { Repository } from '../bindings/crab.gitsync/internal/gitengine/models'

export type RepositoryMenu = { repo: Repository, x: number, y: number }
const labels: Record<string,string> = {fetch:'拉取',merge:'合并',discard:'撤销本地修改','pull-merge':'拉取并合并','abort-merge':'中止合并'}

export default function RepositoryActions({ target, busy, close, run }: {target:RepositoryMenu,busy:boolean,close:()=>void,run:(action:string,branch:string)=>Promise<void>}) {
  const [action,setAction] = useState('')
  const [branches,setBranches] = useState<string[]>([])
  const [branch,setBranch] = useState('')
  const [loading,setLoading] = useState(false)
  const [pending,setPending] = useState(false)
  const [confirmed,setConfirmed] = useState(false)
  const [error,setError] = useState('')
  const panel = useRef<HTMLDivElement>(null)
  const first = useRef<HTMLButtonElement>(null)
  const {repo} = target
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null
    first.current?.focus()
    const key = (event:KeyboardEvent) => {
      if (event.key === 'Escape' && !pending) close()
      if (event.key === 'Tab' && panel.current) {
        const elements = Array.from(panel.current.querySelectorAll<HTMLElement>('button:not(:disabled),select:not(:disabled),input:not(:disabled)'))
        const current = elements.indexOf(document.activeElement as HTMLElement)
        if (elements.length) {event.preventDefault(); elements[(current + (event.shiftKey ? elements.length - 1 : 1)) % elements.length]?.focus()}
      }
    }
    const outside = (event:MouseEvent) => {if (!action && panel.current && !panel.current.contains(event.target as Node)) close()}
    document.addEventListener('keydown',key);document.addEventListener('mousedown',outside)
    return () => {document.removeEventListener('keydown',key);document.removeEventListener('mousedown',outside);previous?.focus()}
  }, [action,pending,close])
  useEffect(() => {
    if (action !== 'merge') return
    let stopped = false
    setLoading(true)
    void GitService.GetBranches(repo.path).then(values => {
      if (stopped) return
      const choices = (values || []).filter(value => value !== `refs/heads/${repo.branch}`)
      setBranches(choices);setBranch(choices.includes(`refs/remotes/${repo.upstream}`) ? `refs/remotes/${repo.upstream}` : choices[0] || '')
    }).catch(e => {if (!stopped) setError(String(e))}).finally(() => {if (!stopped) setLoading(false)})
    return () => {stopped = true}
  }, [action,repo.path,repo.branch,repo.upstream])
  const execute = async (chosen:string) => {
    setPending(true);setError('')
    try {await run(chosen,branch);close()} catch(e) {setError(String(e))} finally {setPending(false)}
  }
  if (!action) return <div ref={panel} className="repository-menu" role="menu" aria-label={`${repo.name} 仓库操作`} style={{left:Math.max(8,Math.min(target.x,window.innerWidth-235)),top:Math.max(8,Math.min(target.y,window.innerHeight-300))}}>
    <div className="repository-menu-title">{repo.name}<small>{repo.branch}</small></div>
    {Object.entries(labels).map(([value,label],index) => <button ref={index===0 ? first : undefined} role="menuitem" key={value} className={value==='discard' ? 'destructive' : ''} disabled={busy || (value==='fetch' ? !repo.remotes?.length : repo.bare || repo.detached) || (value==='pull-merge' && !repo.upstream) || (value==='abort-merge' ? !repo.mergeInProgress : value!=='fetch' && repo.mergeInProgress)} onClick={() => {if (value==='fetch') void execute(value);else {setAction(value);setConfirmed(false);setError('')}}}>{label}{value==='fetch' && <small>仅获取远端更新</small>}</button>)}
    {error && <p role="alert">{error}</p>}
  </div>
  return <div className="dialog-overlay"><div ref={panel} className="action-dialog" role="dialog" aria-modal="true" aria-labelledby="action-title">
    <h2 id="action-title">{labels[action]}</h2><p className="action-repository"><strong>{repo.name}</strong><span>{repo.path}</span></p>
    <p>当前分支：<strong>{repo.branch}</strong></p>
    {action==='merge' && <label className="branch-picker">将以下分支合并到当前分支<select autoFocus aria-label="合并目标分支" disabled={loading || pending} value={branch} onChange={e => setBranch(e.target.value)}>{!branches.length && <option value="">{loading ? '正在读取分支…' : '没有其他可合并分支'}</option>}{branches.map(value => <option key={value} value={value}>{value.startsWith('refs/heads/') ? `本地 · ${value.slice(11)}` : `远端 · ${value.slice(13)}`}</option>)}</select></label>}
    {action==='pull-merge' && <p>获取远端更新后，将 <strong>{repo.upstream || '未设置跟踪分支'}</strong> 合并到 <strong>{repo.branch}</strong>。</p>}
    {['merge','pull-merge'].includes(action) && <p className="action-impact">此操作会更新工作区，可能生成合并提交。请先保存本地修改。发生冲突时会保留现场，可解决冲突后提交或中止合并。</p>}
    {action==='discard' && <><p className="action-impact destructive">已跟踪文件的暂存及工作区修改将被撤销，恢复到最近一次提交，无法通过此工具恢复。未跟踪文件、新增文件和本地提交会保留。</p><label className="discard-confirm"><input type="checkbox" checked={confirmed} disabled={pending} onChange={e => setConfirmed(e.target.checked)}/>我确认撤销此仓库已跟踪文件的本地修改</label></>}
    {action==='abort-merge' && <p className="action-impact">中止当前未完成的合并并尝试恢复合并前状态。解决冲突过程中产生的修改也会撤销。</p>}
    {error && <p className="text-red" role="alert">{error}</p>}
    <div className="dialog-actions"><button ref={first} disabled={pending} onClick={close}>取消</button><button className={action==='discard' ? 'danger-button' : 'primary'} disabled={busy || pending || loading || (action==='merge' && !branch) || (action==='discard' && !confirmed)} onClick={() => void execute(action)}>{pending ? '正在提交任务…' : `确认${labels[action]}`}</button></div>
  </div></div>
}
