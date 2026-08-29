// 控制台主应用：机器人 / 自建应用 / 回调任务 / 消息流水。
import { Fragment, useEffect, useState, useCallback } from 'react';
import type { Bot, AgentView, CallbackTask, Chat, Message } from '../shared/types';
import { api } from '../shared/api';

// 复制按钮：点击复制文本，短暂显示"已复制"。
function CopyBtn({ text }: { text: string }) {
  const [ok, setOk] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      const ta = document.createElement('textarea');
      ta.value = text;
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      ta.remove();
    }
    setOk(true);
    setTimeout(() => setOk(false), 1200);
  };
  return <button className="copy" onClick={copy} title="复制">{ok ? '已复制' : '复制'}</button>;
}

// 一行"字段名 + code + 复制按钮"。
function KV({ k, v, mono }: { k: string; v: string; mono?: boolean }) {
  return (
    <tr>
      <th>{k}</th>
      <td>
        <span className="kv">
          {mono !== false ? <code>{v}</code> : <span>{v}</span>}
          <CopyBtn text={v} />
        </span>
      </td>
    </tr>
  );
}

function TaskStatusBadge({ status }: { status: string }) {
  const cls = { pending: 'st-pending', processing: 'st-processing', streaming: 'st-streaming', done: 'st-done', dead: 'st-dead' }[status] || 'st-pending';
  return <span className={`badge ${cls}`}>{status}</span>;
}

function BotPanel() {
  const [bots, setBots] = useState<Bot[]>([]);
  const [chats, setChats] = useState<Chat[]>([]);
  const [name, setName] = useState('');
  const [url, setUrl] = useState<Record<number, string>>({});
  const [mode, setMode] = useState<Record<number, string>>({});
  const [joinChat, setJoinChat] = useState<Record<number, number>>({});
  const [res, setRes] = useState<Record<number, string>>({});

  const load = useCallback(async () => {
    const [b, c] = await Promise.all([api.admin.bots(), api.admin.chats()]);
    const bl = b || [];
    setBots(bl);
    setChats(c || []);
    setUrl(p => { const n = { ...p }; bl.forEach(x => { if (!(x.id in n)) n[x.id] = x.callback_url || ''; }); return n; });
    setMode(p => { const n = { ...p }; bl.forEach(x => { if (!(x.id in n)) n[x.id] = x.callback_mode || 'encrypted'; }); return n; });
  }, []);
  useEffect(() => { load(); }, [load]);

  const create = async () => {
    if (!name.trim()) return;
    await api.admin.createBot(name.trim());
    setName('');
    load();
  };
  const save = async (id: number) => {
    const r: any = await api.admin.saveBotCallback({ bot_id: id, url: (url[id] || '').trim(), mode: mode[id] || 'encrypted' });
    setRes(p => ({ ...p, [id]: r.verified ? '✅ 验证通过' : '❌ 验证失败: ' + (r.error || '') }));
    load();
  };
  const join = async (id: number) => {
    const chatID = joinChat[id] || (chats[0]?.id ?? 0);
    if (!chatID) return;
    await api.admin.joinChat({ chat_id: chatID, bot_id: id });
    load();
  };

  return (
    <section>
      <h2>机器人</h2>
      {bots.map(b => (
        <table key={b.id} className="kv-table">
          <tbody>
            <tr><th>名称</th><td>{b.name}
              {b.callback_verified ? <span className="badge ok"> 回调已验证</span> : <span className="badge bad"> 回调未验证</span>}
            </td></tr>
            <KV k="aibotid" v={b.aibotid} />
            <KV k="Token" v={b.callback_token} />
            <KV k="EncodingAESKey" v={b.callback_aes_key} />
            <KV k="长连接密钥" v={b.secret} />
            <tr><th>回调 URL</th><td>{b.callback_url ? <><code>{b.callback_url}</code>（{b.callback_mode}）</> : <span className="muted">未配置</span>}</td></tr>
            <tr><th>webhook</th><td>
              {(b.keys || []).length ? (b.keys || []).map(k => {
                const full = `${location.origin}/cgi-bin/webhook/send?key=${k.webhook_key}`;
                return (
                  <div key={k.chat_id} className="kv webhook-line">
                    <span className="chat-label">{k.name}:</span>
                    <code>{full}</code>
                    <CopyBtn text={full} />
                  </div>
                );
              }) : <span className="muted">未入群</span>}
            </td></tr>
            <tr><th>回调配置</th><td>
              <div className="row">
                <input type="text" placeholder="回调 URL，如 http://127.0.0.1:9000/wecom"
                  value={url[b.id] ?? ''} onChange={e => setUrl(p => ({ ...p, [b.id]: e.target.value }))} />
                <select value={mode[b.id] ?? 'encrypted'} onChange={e => setMode(p => ({ ...p, [b.id]: e.target.value }))}>
                  <option value="encrypted">encrypted</option>
                  <option value="plain">plain</option>
                </select>
                <button onClick={() => save(b.id)}>保存并验证</button>
              </div>
              {res[b.id] && <pre className="muted">{res[b.id]}</pre>}
            </td></tr>
            <tr><th>加入群</th><td>
              <div className="row">
                <select value={joinChat[b.id] ?? (chats[0]?.id ?? 0)}
                  onChange={e => setJoinChat(p => ({ ...p, [b.id]: Number(e.target.value) }))}>
                  {chats.map(c => <option key={c.id} value={c.id}>{c.name}（{c.type}）</option>)}
                </select>
                <button className="ghost" onClick={() => join(b.id)}>加入</button>
              </div>
            </td></tr>
          </tbody>
        </table>
      ))}
      <div className="row">
        <input type="text" placeholder="新机器人名称" value={name}
          onChange={e => setName(e.target.value)} onKeyDown={e => e.key === 'Enter' && create()} />
        <button onClick={create}>创建机器人</button>
      </div>
    </section>
  );
}

function AgentPanel() {
  const [agents, setAgents] = useState<AgentView[]>([]);
  const [name, setName] = useState('');
  const [url, setUrl] = useState<Record<number, string>>({});
  const [mode, setMode] = useState<Record<number, string>>({});
  const [res, setRes] = useState<Record<number, string>>({});
  const load = useCallback(async () => {
    const al = (await api.admin.agents()) || [];
    setAgents(al);
    setUrl(p => { const n = { ...p }; al.forEach(x => { if (!(x.agent.id in n)) n[x.agent.id] = x.agent.callback_url || ''; }); return n; });
    setMode(p => { const n = { ...p }; al.forEach(x => { if (!(x.agent.id in n)) n[x.agent.id] = x.agent.callback_mode || 'encrypted'; }); return n; });
  }, []);
  useEffect(() => { load(); }, [load]);
  const create = async () => {
    if (!name.trim()) return;
    await api.admin.createAgent(name.trim());
    setName(''); load();
  };
  const save = async (id: number) => {
    const r: any = await api.admin.saveAgentCallback({ agent_id: id, url: (url[id] || '').trim(), mode: mode[id] || 'encrypted' });
    setRes(p => ({ ...p, [id]: r.verified ? '✅ 验证通过' : '❌ 验证失败: ' + (r.error || '') }));
    load();
  };
  return (
    <section>
      <h2>自建应用（gettoken / message/send）</h2>
      {agents.map(x => { const a = x.agent; return (
        <table key={a.id} className="kv-table">
          <tbody>
            <tr><th>名称</th><td>{a.name}
              {a.callback_verified ? <span className="badge ok"> 回调已验证</span> : <span className="badge bad"> 回调未验证</span>}
            </td></tr>
            <KV k="agentid" v={String(a.agentid)} />
            <KV k="corpid" v={x.corpid} />
            <KV k="corpsecret" v={a.corpsecret} />
            <KV k="gettoken" v={x.gettoken} />
            <KV k="Token" v={a.callback_token} />
            <KV k="EncodingAESKey" v={a.callback_aes_key} />
            <tr><th>回调配置</th><td><div className="row">
              <input type="text" placeholder="回调 URL（自建应用 XML 回调）"
                value={url[a.id] ?? ''} onChange={e => setUrl(p => ({ ...p, [a.id]: e.target.value }))} />
              <select value={mode[a.id] ?? 'encrypted'} onChange={e => setMode(p => ({ ...p, [a.id]: e.target.value }))}>
                <option value="encrypted">encrypted</option>
                <option value="plain">plain</option>
              </select>
              <button onClick={() => save(a.id)}>保存并验证</button>
            </div>
            {res[a.id] && <pre className="muted">{res[a.id]}</pre>}
            </td></tr>
          </tbody>
        </table>
      ); })}
      <div className="row">
        <input type="text" placeholder="新自建应用名称" value={name}
          onChange={e => setName(e.target.value)} onKeyDown={e => e.key === 'Enter' && create()} />
        <button onClick={create}>创建自建应用</button>
      </div>
    </section>
  );
}

function TaskPanel() {
  const [status, setStatus] = useState('');
  const [tasks, setTasks] = useState<CallbackTask[]>([]);
  const [open, setOpen] = useState<Record<number, boolean>>({});
  const load = useCallback(async () => { setTasks((await api.admin.tasks(status)) || []); }, [status]);
  useEffect(() => { load(); const t = setInterval(load, 4000); return () => clearInterval(t); }, [load]);

  const prettyPayload = (p: string) => {
    try { return JSON.stringify(JSON.parse(p), null, 2); } catch { return p; }
  };

  return (
    <section>
      <h2>回调任务（推送状态 / 重放）</h2>
      <div className="row">
        <select value={status} onChange={e => setStatus(e.target.value)}>
          <option value="">全部</option>
          <option value="pending">pending</option>
          <option value="processing">processing</option>
          <option value="streaming">streaming</option>
          <option value="done">done</option>
          <option value="dead">dead</option>
        </select>
        <button onClick={load}>刷新</button>
      </div>
      {tasks.length ? (
        <table>
          <thead><tr><th>ID</th><th>状态</th><th>尝试</th><th>目标</th><th>最近错误</th><th>操作</th></tr></thead>
          <tbody>
            {tasks.map(t => (
              <Fragment key={t.id}>
                <tr>
                  <td>{t.id}</td>
                  <td><TaskStatusBadge status={t.status} /></td>
                  <td>{t.attempt}</td>
                  <td>{t.target_type}#{t.target_id}</td>
                  <td className="muted err-cell">{t.last_error || ''}</td>
                  <td className="row nowrap">
                    <button className="ghost" onClick={() => api.admin.replayTask(t.id).then(load)}>重放</button>
                    <button className="ghost" onClick={() => setOpen(p => ({ ...p, [t.id]: !p[t.id] }))}>
                      {open[t.id] ? '收起' : '报文'}
                    </button>
                  </td>
                </tr>
                {open[t.id] && (
                  <tr>
                    <td colSpan={6}><pre>{prettyPayload(t.payload)}</pre></td>
                  </tr>
                )}
              </Fragment>
            ))}
          </tbody>
        </table>
      ) : <div className="muted">无</div>}
    </section>
  );
}

function MessagePanel() {
  const [chats, setChats] = useState<Chat[]>([]);
  const [chatId, setChatId] = useState<number>(0);
  const [msgs, setMsgs] = useState<Message[]>([]);
  const loadChats = useCallback(async () => {
    const c = (await api.admin.chats()) || [];
    setChats(c);
    if (!chatId && c.length) setChatId(c[0].id);
  }, [chatId]);
  const loadMsgs = useCallback(async () => {
    if (chatId) setMsgs((await api.admin.messages(chatId)) || []);
  }, [chatId]);
  useEffect(() => { loadChats(); }, [loadChats]);
  useEffect(() => { loadMsgs(); const t = setInterval(loadMsgs, 4000); return () => clearInterval(t); }, [loadMsgs]);

  const pretty = (v: any) => JSON.stringify(v, null, 2);

  return (
    <section>
      <h2>消息流水</h2>
      <div className="row">
        <select value={chatId} onChange={e => setChatId(Number(e.target.value))}>
          {chats.map(c => <option key={c.id} value={c.id}>{c.name}（{c.type}）</option>)}
        </select>
        <button onClick={loadMsgs}>刷新</button>
        <a className="btn-link" href={api.exportCsv(chatId)} download>导出 CSV</a>
      </div>
      {msgs.length ? (
        <table>
          <thead><tr><th>msgid</th><th>时间</th><th>发送者</th><th>类型</th><th>内容</th><th>提及</th></tr></thead>
          <tbody>
            {msgs.map(m => (
              <tr key={m.id}>
                <td><code>{m.msgid}</code></td>
                <td className="nowrap muted">{new Date(m.ts * 1000).toLocaleString('zh-CN')}</td>
                <td>{m.sender}{m.sender_type === 'bot' ? ' (bot)' : ''}</td>
                <td><span className="badge st-type">{m.msgtype}</span></td>
                <td><pre>{pretty(m.content)}</pre></td>
                <td className="muted">{(m.mentioned || []).join(', ')}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : <div className="muted">无</div>}
    </section>
  );
}

export function App() {
  return (
    <>
      <header>
        <strong>本地 IM 控制台</strong>
        <a href="/">← 回到群聊</a>
      </header>
      <main>
        <BotPanel />
        <AgentPanel />
        <TaskPanel />
        <MessagePanel />
      </main>
    </>
  );
}
