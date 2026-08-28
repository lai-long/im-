// fetch 封装：JSON GET/POST，失败返回 {errcode:-1,...} 而非抛错（前端健壮性）。
// 所有用户可控文本由 React 自动转义，不再手写 escape。

import type { User, Chat, Bot, AgentView, CallbackTask, Message } from './types';

async function jget<T = any>(url: string): Promise<T> {
  try {
    const r = await fetch(url);
    return await r.json();
  } catch (e) {
    return { errcode: -1, errmsg: (e as Error).message } as any;
  }
}

async function jpost<T = any>(url: string, body: any): Promise<T> {
  try {
    const r = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    return await r.json();
  } catch (e) {
    return { errcode: -1, errmsg: (e as Error).message } as any;
  }
}

export const api = {
  // 客户端
  users: () => jget<User[]>('/api/users'),
  login: (body: { userid?: string; name?: string }) => jpost<User>('/api/login', body),
  chats: (userid: string) => jget<Chat[]>(`/api/chats?userid=${encodeURIComponent(userid)}`),
  bots: () => jget<Bot[]>('/api/bots'),
  messages: (chatID: number, limit = 100) =>
    jget<Message[]>(`/api/messages?chat_id=${chatID}&limit=${limit}`),
  send: (body: { userid: string; text: string; chat_id: number }) =>
    jpost('/api/send', body),
  openSingleChat: (body: { userid: string; bot_id: number }) =>
    jpost<Chat>('/api/chats/single', body),
  cardInteract: (body: { userid: string; msgid: string; event_key: string; button_selection?: any }) =>
    jpost('/api/card/interact', body),
  exportCsv: (chatID: number) => `/api/export?chat_id=${chatID}&format=csv`,
  replay: (body: { userid: string; chat_id: number }) =>
    jpost<{ errcode: number; count: number }>('/api/replay', body),

  // 控制台
  admin: {
    bots: () => jget<Bot[]>('/admin/bots'),
    createBot: (name: string, chatID?: number) =>
      jpost<Bot>('/admin/bots', { name, chat_id: chatID ?? 0 }),
    saveBotCallback: (body: { bot_id: number; url: string; mode: string }) =>
      jpost('/admin/bots/callback', body),
    joinChat: (body: { chat_id: number; bot_id: number }) =>
      jpost('/admin/chats/join', body),
    chats: () => jget<Chat[]>('/admin/chats'),
    agents: () => jget<AgentView[]>('/admin/agents'),
    createAgent: (name: string) => jpost('/admin/agents', { name }),
    saveAgentCallback: (body: { agent_id: number; url: string; mode: string }) =>
      jpost('/admin/agents/callback', body),
    tasks: (status: string) =>
      jget<CallbackTask[]>(`/admin/tasks${status ? `?status=${status}` : ''}`),
    replayTask: (id: number) => jpost('/admin/tasks/replay', { id }),
    messages: (chatID: number, limit = 200) =>
      jget<Message[]>(`/admin/messages?chat_id=${chatID}&limit=${limit}`),
  },
};
