import { useEffect, useRef, useState } from 'react'
import { GitService } from '../bindings/crab.gitsync'
import { Browser, Clipboard } from '@wailsio/runtime'
import type { DiscardBackup, DiscardPreview, MergePreview, Repository } from '../bindings/crab.gitsync/internal/gitengine/models'

export type RepositoryMenu = { repo: Repository, x: number, y: number }
const labels: Record<string,string> = {fetch:'拉取',merge:'合并',discard:'撤销本地修改','restore-discard':'恢复撤销备份','pull-merge':'拉取并合并','abort-merge':'中止合并'}

export default function RepositoryActions({ target, busy, close, run }: {target:RepositoryMenu,busy:boolean,close:()=>void,run:(action:string,branch:string,preview?:MergePreview,strategy?:string,operation?:()=>Promise<void>)=>Promise<void>}) {
  const [action,setAction] = useState('')
  const [branches,setBranches] = useState<string[]>([])
  const [branch,setBranch] = useState('')
  const [loading,setLoading] = useState(false)
  const [preview,setPreview] = useState<MergePreview | null>(null)
  const [previewLoading,setPreviewLoading] = useState(false)
  const [strategy,setStrategy] = useState('ff-only')
  const [pending,setPending] = useState(false)
  const [confirmed,setConfirmed] = useState(false)
  const [error,setError] = useState('')
  const [fetchRemote,setFetchRemote]=useState('')
  const [message,setMessage]=useState('')
  const [discardPreview,setDiscardPreview]=useState<DiscardPreview|null>(null)
  const [files,setFiles]=useState<string[]>([])
  const [backup,setBackup]=useState(false)
  const [backups,setBackups]=useState<DiscardBackup[]>([])
  const [backupID,setBackupID]=useState('')
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
  useEffect(()=>{
    setPreview(null);setConfirmed(false)
    if(!['merge','pull-merge'].includes(action) || (action==='merge' && !branch))return
    let stopped=false
    setPreviewLoading(true);setError('')
    void GitService.PreviewMerge(repo.path,action,branch).then(value=>{if(!stopped)setPreview(value)}).catch(e=>{if(!stopped)setError(String(e))}).finally(()=>{if(!stopped)setPreviewLoading(false)})
    return()=>{stopped=true}
  },[action,branch,repo.path])
  useEffect(()=>{
    if(action!=='discard' && action!=='restore-discard')return
    let stopped=false
    setLoading(true);setError('');setFiles([]);setBackup(false);setDiscardPreview(null);setBackups([]);setBackupID('')
    const load=async()=>{
      if(action==='discard') {const p=await GitService.GetDiscardPreview(repo.path);if(!stopped)setDiscardPreview(p)}
      else {const values=await GitService.GetDiscardBackups(repo.path)||[];if(!stopped)setBackups(values)}
    }
    void load().catch(e=>{if(!stopped)setError(String(e))}).finally(()=>{if(!stopped)setLoading(false)})
    return()=>{stopped=true}
  },[action,repo.path])
  const selectedBackup=backups.find(value=>value.id===backupID)
  const discardFiles=discardPreview?.files||[]
  const merging=['merge','pull-merge'].includes(action)
  const execute = async (chosen:string) => {
    setPending(true);setError('')
    try {
      let operation:(()=>Promise<void>)|undefined
      if(chosen==='discard') {if(!discardPreview || !confirmed || !files.length)throw new Error('请选择文件并确认');operation=()=>GitService.StartDiscard(discardPreview,files,backup,true)}
      if(chosen==='restore-discard') {if(!selectedBackup?.ready || !confirmed)throw new Error('请选择就绪备份并确认');operation=()=>GitService.StartRestoreDiscard(repo.path,backupID,true)}
      await run(chosen,chosen==='fetch'?fetchRemote:branch,merging ? preview || undefined : undefined,strategy,operation);close()
    } catch(e) {setError(String(e))} finally {setPending(false)}
  }
  const shortcut=async(kind:string)=>{
    setPending(true);setError('');setMessage('')
    try{
      if(kind==='folder'||kind==='terminal')await GitService.OpenRepository(repo.path,kind)
      else {const links=await GitService.GetRepositoryLinks(repo.path)||[];const selected=links.find(value=>value.name===fetchRemote)||links.find(value=>value.name==='origin')||links[0];if(!selected)throw new Error('此仓库未配置远端');if(kind==='github'){if(!selected.githubURL)throw new Error('所选远端不是有效的 GitHub 仓库');await Browser.OpenURL(selected.githubURL)}else{await Clipboard.SetText(selected.address);setMessage(`已复制 ${selected.name} 的脱敏远端地址`)}}
    }catch(e){setError(String(e))}finally{setPending(false)}
  }
  if (!action) return <div ref={panel} className="repository-menu" role="menu" aria-label={`${repo.name} 仓库操作`} style={{left:Math.max(8,Math.min(target.x,window.innerWidth-235)),top:Math.max(8,Math.min(target.y,window.innerHeight-560)),maxHeight:'calc(100vh - 16px)',overflowY:'auto'}}>
    <div className="repository-menu-title">{repo.name}<small>{repo.branch}</small></div>
    <label>获取远端<select aria-label="拉取远端" disabled={busy||pending} value={fetchRemote} onChange={e=>setFetchRemote(e.target.value)}><option value="">全部远端（默认）</option>{repo.remotes?.map(remote=><option value={remote.name} key={remote.name}>{remote.name}</option>)}</select></label>
    {Object.entries(labels).map(([value,label],index) => <button ref={index===0 ? first : undefined} role="menuitem" key={value} className={value==='discard' ? 'destructive' : ''} disabled={busy || (value==='fetch' ? !repo.remotes?.length : repo.bare || repo.detached) || (value==='pull-merge' && !repo.upstream) || (value==='abort-merge' ? !repo.mergeInProgress : value!=='fetch' && repo.mergeInProgress)} onClick={() => {if (value==='fetch') void execute(value);else {setAction(value);setStrategy('ff-only');setConfirmed(false);setError('')}}}>{label}{value==='fetch' && <small>仅获取远端更新</small>}</button>)}
    {error && <p role="alert">{error}</p>}
    {['folder','terminal','github','copy'].map(kind=><button role="menuitem" key={kind} disabled={busy||pending} onClick={()=>void shortcut(kind)}>{{folder:'打开仓库目录',terminal:'在此打开终端',github:'打开 GitHub 页面',copy:'复制远端地址'}[kind]}</button>)}
    <small>页面和复制使用所选远端；全部远端时优先 origin。</small>
    {message&&<p role="status">{message}</p>}
  </div>
  return <div className="dialog-overlay"><div ref={panel} className="action-dialog" role="dialog" aria-modal="true" aria-labelledby="action-title">
    <h2 id="action-title">{labels[action]}</h2><p className="action-repository"><strong>{repo.name}</strong><span>{repo.path}</span></p>
    <p>当前分支：<strong>{repo.branch}</strong></p>
    {action==='merge' && <label className="branch-picker">将以下分支合并到当前分支<select autoFocus aria-label="合并目标分支" disabled={loading || pending} value={branch} onChange={e => setBranch(e.target.value)}>{!branches.length && <option value="">{loading ? '正在读取分支…' : '没有其他可合并分支'}</option>}{branches.map(value => <option key={value} value={value}>{value.startsWith('refs/heads/') ? `本地 · ${value.slice(11)}` : `远端 · ${value.slice(13)}`}</option>)}</select></label>}
    {action==='pull-merge' && <p>获取远端更新后，将 <strong>{repo.upstream || '未设置跟踪分支'}</strong> 合并到 <strong>{repo.branch}</strong>。</p>}
    {merging && <>
      <label className="branch-picker">合并策略<select aria-label="合并策略" disabled={pending} value={strategy} onChange={e=>{setStrategy(e.target.value);setConfirmed(false)}}><option value="ff-only">仅快进（默认，不生成合并提交）</option><option value="merge">普通合并（明确允许合并提交和冲突）</option></select></label>
      {previewLoading && <p role="status">正在读取提交关系…</p>}
      {preview && <div className="merge-preview"><p>源分支：<strong>{preview.target}</strong><small>{preview.targetHead.slice(0,12)}</small></p><p>目标分支：<strong>{preview.branch}</strong><small>{preview.head.slice(0,12)}</small></p><p>当前分支领先 {preview.ahead} 个提交，落后 {preview.behind} 个提交{preview.diverged ? ' · 双方分叉' : ''}</p>{action==='pull-merge' && <p>以上为本地引用的预览，获取后会重新检查提交关系。普通合并目标变化时需重新预览确认。</p>}{preview.diverged && strategy==='ff-only' && <p className="text-amber">双方分叉，无法仅快进。可取消操作，或明确选择普通合并。</p>}</div>}
      <p className="action-impact">此操作会更新工作区。仅快进无法完成时停止，不自动暂存或变基；普通合并发生冲突时保留现场。</p>
      {strategy==='merge' && <label className="discard-confirm"><input type="checkbox" checked={confirmed} disabled={pending} onChange={e=>setConfirmed(e.target.checked)}/>我确认普通合并，允许生成合并提交并处理可能的冲突</label>}
    </>}
    {action==='discard' && <>
      <p className="action-impact destructive">仅所选文件的暂存及工作区修改恢复到 HEAD。未选文件、新增/未跟踪文件和本地提交保留。未开启备份时，此工具无法恢复所选修改。</p>
      {loading?<p role="status">正在读取受影响文件…</p>:<>
        <p>可选 {discardFiles.length} 个文件 · 已选择 {files.length} 个</p>
        {!discardFiles.length?<p>没有可撤销的已跟踪文件修改。</p>:<>
          <button disabled={pending} onClick={()=>{setFiles(discardFiles.map(f=>f.path));setConfirmed(false)}}>全选</button>{' '}
          <button disabled={pending} onClick={()=>{setFiles([]);setConfirmed(false)}}>清空选择</button>
          <div className="discard-files" aria-label="撤销文件列表">{discardFiles.map(f=><label key={f.path}><input type="checkbox" checked={files.includes(f.path)} disabled={pending} onChange={e=>{setFiles(values=>e.target.checked?[...values,f.path]:values.filter(value=>value!==f.path));setConfirmed(false)}}/><span>{f.path}</span></label>)}</div>
        </>}
      </>}
      <label className="discard-confirm"><input type="checkbox" checked={backup} disabled={pending} onChange={e=>{setBackup(e.target.checked);setConfirmed(false)}}/>撤销前保存本地备份（包含所选文件的工作区与暂存内容）</label>
      <p>备份保存在应用配置目录，可从仓库菜单恢复。备份写入失败会停止撤销；恢复前若文件、分支或 HEAD 已变化，将拒绝覆盖。最多 500 个候选文件，所选备份内容合计不超过 64 MiB。</p>
      <label className="discard-confirm"><input type="checkbox" checked={confirmed} disabled={pending||!files.length} onChange={e=>setConfirmed(e.target.checked)}/>我确认撤销所选 {files.length} 个文件的暂存及工作区修改</label>
    </>}
    {action==='restore-discard' && <>
      {loading?<p role="status">正在读取本地备份…</p>:<label className="branch-picker">选择备份<select aria-label="撤销备份" disabled={pending} value={backupID} onChange={e=>{setBackupID(e.target.value);setConfirmed(false)}}><option value="">{backups.length?'请选择备份':'此仓库没有本地备份'}</option>{backups.map(value=><option key={value.id} value={value.id} disabled={!value.ready}>{new Date(value.createdAt).toLocaleString()} · {value.files?.length||0} 个文件{value.ready?'':' · 尚未就绪'}</option>)}</select></label>}
      {selectedBackup&&<><p>备份：{selectedBackup.id}</p><ul className="discard-files">{(selectedBackup.files||[]).map(file=><li key={file}>{file}</li>)}</ul></>}
      <p className="action-impact">恢复所列文件原来的工作区与暂存内容，保留其他文件。撤销后有新修改或分支/HEAD 改变时停止；原始备份持续保留。</p>
      <label className="discard-confirm"><input type="checkbox" checked={confirmed} disabled={pending||!selectedBackup?.ready} onChange={e=>setConfirmed(e.target.checked)}/>我确认恢复所选备份中的文件</label>
    </>}
    {action==='abort-merge' && <p className="action-impact">中止当前未完成的合并并尝试恢复合并前状态。解决冲突过程中产生的修改也会撤销。</p>}
    {error && <p className="text-red" role="alert">{error}</p>}
    <div className="dialog-actions"><button ref={first} disabled={pending} onClick={close}>取消</button><button className={action==='discard' ? 'danger-button' : 'primary'} disabled={busy || pending || loading || (merging && (previewLoading || !preview || (strategy==='merge' && !confirmed) || (strategy==='ff-only' && preview.diverged))) || (action==='merge' && !branch) || (action==='discard' && (!confirmed||!discardPreview||!files.length)) || (action==='restore-discard' && (!confirmed||!selectedBackup?.ready))} onClick={() => void execute(action)}>{pending ? '正在提交任务…' : `确认${labels[action]}`}</button></div>
  </div></div>
}
