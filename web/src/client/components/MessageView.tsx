// 消息渲染：按 msgtype 分支，含模板卡片交互按钮（事件委托到 #msgs）。
// 流式消息由父组件按 msgid 覆盖更新后传入，这里只负责渲染单条。

import type { Message } from '../../shared/types';
import { api } from '../../shared/api';

const userID = () => JSON.parse(localStorage.getItem('im-user') || '{}').userid as string;

// 文本中的 @userid 高亮（拆分渲染，避免 dangerouslySetInnerHTML）。
function TextWithMentions({ text }: { text: string }) {
  const parts: React.ReactNode[] = [];
  const re = /(@[^\s，,。@]+)/g;
  let last = 0;
  let i = 0;
  let m: RegExpExecArray | null;
  while ((m = re.exec(text)) !== null) {
    if (m.index > last) parts.push(text.slice(last, m.index));
    parts.push(<span key={i++} className="at">{m[0]}</span>);
    last = m.index + m[0].length;
  }
  if (last < text.length) parts.push(text.slice(last));
  return <>{parts}</>;
}

function Body({ m }: { m: Message }) {
  const c = m.content || {};
  switch (m.msgtype) {
    case 'image':
      return <img className="msg-img" src={`data:image/png;base64,${c.base64 || ''}`} alt="" />;
    case 'news': {
      const arts: any[] = Array.isArray(c.articles) ? c.articles : [];
      return (
        <>
          {arts.map((a, i) => (
            <div className="news" key={i}>
              <a href={a.url || '#'} target="_blank" rel="noreferrer">{a.title || ''}</a>
              <div className="news-desc">{a.description || ''}</div>
            </div>
          ))}
        </>
      );
    }
    case 'template_card': {
      const title = c.main_title?.title || '';
      const desc = c.main_title?.desc || '';
      const hlist: any[] = Array.isArray(c.horizontal_content_list) ? c.horizontal_content_list : [];
      const btns: any[] = Array.isArray(c.button_list) ? c.button_list : [];
      const sels: any[] = Array.isArray(c.button_selection) ? c.button_selection : [];
      return (
        <div className="card">
          <b>卡片·{c.card_type || ''}</b>
          <div className="card-title">{title}</div>
          {desc ? <div className="card-desc">{desc}</div> : null}
          {hlist.map((h, i) => (
            <div className="card-h" key={i}>
              <span>{h.keyname || ''}</span>
              <span>{h.value || ''}</span>
            </div>
          ))}
          {btns.length > 0 && (
            <div className="card-btns">
              {btns.map((b, i) => (
                <button key={i} data-card-key={b.key || ''} data-card-msgid={m.msgid}>
                  {b.text || ''}
                </button>
              ))}
            </div>
          )}
          {sels.length > 0 && (
            <>
              {sels.map((sel, i) => (
                <select key={i} data-card-sel="1" data-card-msgid={m.msgid}>
                  {(sel.option_list || []).map((o: any) => (
                    <option key={o.id} value={o.id || ''}>{o.text || ''}</option>
                  ))}
                </select>
              ))}
              <button data-card-sel-confirm="1" data-card-msgid={m.msgid}>确定</button>
            </>
          )}
        </div>
      );
    }
    case 'file':
      return <a href={`/api/media/${c.media_id || ''}`}>📎 文件 {c.media_id || ''}</a>;
    case 'voice':
      return <a href={`/api/media/${c.media_id || ''}`}>🔊 语音 {c.media_id || ''}</a>;
    default:
      return <TextWithMentions text={c.content || ''} />;
  }
}

export function MessageBubble({ m }: { m: Message }) {
  if (m.msgtype === 'event') return null;
  const time = new Date(m.ts / 1000).toLocaleTimeString();
  const typing = m.msgtype === 'stream' && m.content?.finish === false
    ? <span className="typing"> 生成中…</span> : null;
  const tag = m.sender_type === 'bot' ? <span className="bot-tag">bot</span> : null;
  return (
    <div className="msg" data-msgid={m.msgid}>
      <div className="meta">{tag}{m.sender} · {time}{typing}</div>
      <div className="content"><Body m={m} /></div>
    </div>
  );
}

// 列表：事件委托处理卡片按钮点击。
export function MessageList({ messages }: { messages: Message[] }) {
  const onClick = async (e: React.MouseEvent) => {
    const btn = (e.target as HTMLElement).closest('button') as HTMLButtonElement | null;
    if (!btn || !btn.dataset.cardMsgid) return;
    const msgid = btn.dataset.cardMsgid;
    let eventKey = btn.dataset.cardKey || '';
    if (btn.dataset.cardSelConfirm) {
      const sel = (btn.closest('.msg')?.querySelector('select[data-card-sel="1"]')) as HTMLSelectElement | null;
      if (sel) eventKey = sel.value;
    }
    if (!eventKey && !btn.dataset.cardSelConfirm) return;
    await api.cardInteract({ userid: userID(), msgid, event_key: String(eventKey) });
  };
  return (
    <div id="msgs" onClick={onClick}>
      {messages.map(m => <MessageBubble key={m.msgid} m={m} />)}
    </div>
  );
}
